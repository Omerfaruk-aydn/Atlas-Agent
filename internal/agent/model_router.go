package agent

import (
	"context"
	"log/slog"
	"strconv"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/harnessmetrics"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/modelrouter"
)

// routerOutputClip bounds the tool output the router inspects per call; the
// failure markers it looks for appear in the first screenful.
const routerOutputClip = 4000

// routerRuntime is the resolved state of the automatic model router: the
// heuristic, the model for cheap steps and, when a strong role is
// configured, the model for demanding ones. A nil strong means the
// session's own large model.
type routerRuntime struct {
	router *modelrouter.Router
	fast   Model
	strong *Model
}

// SetRouter installs, replaces or (with nil) removes the model router. It is
// safe to call while turns are running; a turn picks the change up at its
// next step.
func (a *sessionAgent) SetRouter(rt *routerRuntime) {
	a.router.Store(rt)
}

func routerPrices(m Model) modelrouter.Prices {
	c := m.CatwalkCfg
	return modelrouter.Prices{
		In:         c.CostPer1MIn,
		Out:        c.CostPer1MOut,
		CacheRead:  c.CostPer1MOutCached,
		CacheWrite: c.CostPer1MInCached,
	}
}

// routerSteps turns finished fantasy steps into the router's view of them.
func routerSteps(steps []fantasy.StepResult) []modelrouter.Step {
	out := make([]modelrouter.Step, 0, len(steps))
	for _, step := range steps {
		var s modelrouter.Step
		for _, r := range step.Content.ToolResults() {
			_, failed := r.Result.(fantasy.ToolResultOutputContentError)
			output := toolResultOutputString(r.Result)
			if len(output) > routerOutputClip {
				output = output[:routerOutputClip]
			}
			s.Calls = append(s.Calls, modelrouter.ToolCall{Name: r.ToolName, Failed: failed, Output: output})
		}
		out = append(out, s)
	}
	return out
}

// hasImageParts reports whether any message carries an image the fast model
// would have to read.
func hasImageParts(messages []fantasy.Message) bool {
	for _, msg := range messages {
		for _, part := range msg.Content {
			if file, ok := fantasy.AsMessagePart[fantasy.FilePart](part); ok && strings.HasPrefix(strings.ToLower(file.MediaType), "image/") {
				return true
			}
		}
	}
	return false
}

// routeStep picks the model for the coming step and points the turn's chain
// at it. It runs at the top of PrepareStep, after the previous step's tool
// results are in the history, so a switch never lands between a tool call
// and its result. A failure to route leaves the session's own model in
// place: routing is an optimisation and must never break a turn.
func (a *sessionAgent) routeStep(sessionID, prompt string, chain *modelChain, large Model, options fantasy.PrepareStepFunctionOptions, contextTokens int64, hasPromptPrefix bool) {
	rt := a.router.Load()
	if rt == nil || a.isSubAgent || hasPromptPrefix {
		chain.Route(nil)
		return
	}
	strong := large
	if rt.strong != nil {
		strong = *rt.strong
	}
	if sameModel(rt.fast, strong) || rt.fast.Model == nil || strong.Model == nil {
		chain.Route(nil)
		return
	}

	decision := rt.router.Decide(sessionID, modelrouter.Input{
		StepNumber:    options.StepNumber,
		Prompt:        prompt,
		Steps:         routerSteps(options.Steps),
		ContextTokens: contextTokens,
		FastWindow:    rt.fast.CatwalkCfg.ContextWindow,
		NeedStrong:    !rt.fast.CatwalkCfg.SupportsImages && hasImageParts(options.Messages),
		Strong:        routerPrices(strong),
		Fast:          routerPrices(rt.fast),
		CrossProvider: rt.fast.ModelCfg.Provider != strong.ModelCfg.Provider,
	})

	chosen := strong
	if decision.Tier == modelrouter.TierFast {
		chosen = rt.fast
	}
	previous := chain.Current()
	if sameModel(chosen, chain.models[0]) {
		chain.Route(nil)
	} else {
		chain.Route(&chosen)
	}
	if decision.Switched {
		slog.Info("Model router switched",
			"from", previous.ModelCfg.Provider+"/"+previous.ModelCfg.Model,
			"to", chosen.ModelCfg.Provider+"/"+chosen.ModelCfg.Model,
			"reason", decision.Reason)
		a.recordHarness(harnessmetrics.RouterSwitch, sessionID, map[string]string{
			"from":   previous.ModelCfg.Model,
			"to":     chosen.ModelCfg.Model,
			"tier":   string(decision.Tier),
			"reason": decision.Reason,
		})
	}
}

// routerStepSaving estimates what a step that ran on the fast model saved
// against the strong model at the same token counts. It is 0 for steps that
// did not run on the fast model, for estimated usage, and when either side
// is billed flat.
func routerStepSaving(used, strong Model, usage fantasy.Usage) float64 {
	if strong.FlatRate {
		return 0
	}
	if used.FlatRate {
		return routerCost(strong, usage)
	}
	return max(0, routerCost(strong, usage)-routerCost(used, usage))
}

func routerCost(m Model, usage fantasy.Usage) float64 {
	return modelrouter.StepCost(routerPrices(m), usage.InputTokens, usage.OutputTokens, usage.CacheReadTokens, usage.CacheCreationTokens)
}

// noteRouterStep records the saving of a finished step when the router had
// moved it to the fast model.
func (a *sessionAgent) noteRouterStep(sessionID string, chain *modelChain, large Model, usage fantasy.Usage, estimated bool) {
	rt := a.router.Load()
	if rt == nil || estimated || chain.routed == nil || chain.active != 0 || !sameModel(*chain.routed, rt.fast) {
		return
	}
	strong := large
	if rt.strong != nil {
		strong = *rt.strong
	}
	saved := routerStepSaving(rt.fast, strong, usage)
	a.recordHarness(harnessmetrics.RouterStep, sessionID, map[string]string{
		"model": rt.fast.ModelCfg.Model,
		"saved": strconv.FormatFloat(saved, 'f', 6, 64),
	})
}

// routerSetter is implemented by session agents that can route models. The
// SessionAgent interface stays unchanged so test doubles need no new method.
type routerSetter interface {
	SetRouter(*routerRuntime)
}

// buildRouter resolves options.router into a runtime, or nil when the router
// is off or its fast model cannot be built. A misconfigured router is logged
// and ignored: the session keeps running on its own model.
func (c *coordinator) buildRouter(ctx context.Context) *routerRuntime {
	cfg := c.cfg.Config()
	if cfg.Options == nil || !cfg.Options.Router.IsEnabled() {
		return nil
	}
	opts := cfg.Options.Router
	fastCfg, ok := cfg.ResolveRole(opts.FastRoleName())
	if !ok {
		fastCfg, ok = cfg.ResolveRole(string(config.SelectedModelTypeSmall))
	}
	if !ok {
		slog.Warn("Model router is enabled but no fast model could be resolved; routing is off", "role", opts.FastRoleName())
		return nil
	}
	fast, err := c.resolveModel(ctx, fastCfg, true)
	if err != nil {
		slog.Warn("Model router fast model failed to build; routing is off", "error", err)
		return nil
	}
	rt := &routerRuntime{
		fast: fast,
		router: modelrouter.New(modelrouter.Config{
			MinDwell:        opts.MinDwellSteps,
			DownshiftStreak: opts.DownshiftStreak,
			ErrorStreak:     opts.ErrorStreak,
		}),
	}
	if role := strings.TrimSpace(opts.StrongRole); role != "" {
		strongCfg, ok := cfg.ResolveRole(role)
		if !ok {
			slog.Warn("Model router strong role is not configured; using the session's model", "role", role)
			return rt
		}
		strong, err := c.resolveModel(ctx, strongCfg, false)
		if err != nil {
			slog.Warn("Model router strong model failed to build; using the session's model", "error", err)
			return rt
		}
		rt.strong = &strong
	}
	return rt
}

// applyRouter installs the router on agent when it supports routing.
func (c *coordinator) applyRouter(ctx context.Context, agent SessionAgent) {
	if setter, ok := agent.(routerSetter); ok {
		setter.SetRouter(c.buildRouter(ctx))
	}
}
