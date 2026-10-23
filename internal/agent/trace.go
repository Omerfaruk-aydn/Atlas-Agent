package agent

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm/providers/openrouter"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/harnessmetrics"
	"github.com/google/uuid"
)

type turnTrace struct {
	trace  *harnessmetrics.Trace
	step   atomic.Int64
	closed atomic.Bool
	tools  sync.Map
}

type (
	traceKey        struct{}
	traceCarrierKey struct{}
	traceCarrier    struct {
		mu    sync.Mutex
		trace *turnTrace
	}
)

func (t *turnTrace) startStep(step int) {
	if t == nil {
		return
	}
	t.step.Store(int64(step + 1))
	t.record(harnessmetrics.TraceEvent{Kind: "step_start"})
}

func withTraceCarrier(ctx context.Context) context.Context {
	return context.WithValue(ctx, traceCarrierKey{}, &traceCarrier{})
}

func traceFromContext(ctx context.Context) *turnTrace {
	if t, ok := ctx.Value(traceKey{}).(*turnTrace); ok {
		return t
	}
	if c, ok := ctx.Value(traceCarrierKey{}).(*traceCarrier); ok {
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.trace
	}
	return nil
}

func (a *sessionAgent) beginTurnTrace(ctx context.Context, sessionID string) (context.Context, *turnTrace) {
	if !a.harnessOptions().TraceEnabled() {
		return ctx, nil
	}
	t := &turnTrace{trace: a.harness.BeginTrace(sessionID)}
	if t.trace == nil {
		return ctx, nil
	}
	if c, ok := ctx.Value(traceCarrierKey{}).(*traceCarrier); ok {
		c.mu.Lock()
		c.trace = t
		c.mu.Unlock()
	}
	a.traceRuns.Store(sessionID, t)
	return context.WithValue(ctx, traceKey{}, t), t
}

func (t *turnTrace) record(event harnessmetrics.TraceEvent) {
	if t == nil {
		return
	}
	if event.Step == 0 {
		event.Step = int(t.step.Load())
	}
	t.trace.Record(event)
}

func (a *sessionAgent) sessionTrace(sessionID string) *turnTrace {
	if t, ok := a.traceRuns.Load(sessionID); ok {
		return t.(*turnTrace)
	}
	return nil
}

func (a *sessionAgent) finishTurnTrace(t *turnTrace, sessionID string, started time.Time, err error) {
	if t == nil || t.closed.Swap(true) {
		return
	}
	outcome, reason := "finished", "run_returned_without_transport_error"
	if err != nil {
		outcome, reason = "error", "run_error"
		if errors.Is(err, context.Canceled) {
			outcome, reason = "canceled", "user_canceled"
		}
	}
	t.record(harnessmetrics.TraceEvent{Kind: "turn_end", Outcome: outcome, Reason: reason, DurationMS: time.Since(started).Milliseconds()})
	a.traceRuns.CompareAndDelete(sessionID, t)
}

func traceRetry(ctx context.Context, delay time.Duration, model Model) {
	traceFromContext(ctx).record(harnessmetrics.TraceEvent{Kind: "retry", Name: model.ModelCfg.Provider + "/" + model.ModelCfg.Model, Reason: "provider_retry_scheduled", WaitMS: delay.Milliseconds()})
}

func (a *sessionAgent) recordTraceSignal(sessionID string, kind harnessmetrics.Kind) {
	if kind == harnessmetrics.LoopStopped {
		a.sessionTrace(sessionID).record(harnessmetrics.TraceEvent{Kind: "gate", Name: "loop_detection", Outcome: "unresolved", Reason: "repeated_tool_calls"})
	}
	switch kind {
	case harnessmetrics.Compaction, harnessmetrics.FeatureHandoff, harnessmetrics.LoopStopped,
		harnessmetrics.StepBack, harnessmetrics.Alternatives, harnessmetrics.AlternativesPrepared:
		a.sessionTrace(sessionID).record(harnessmetrics.TraceEvent{Kind: "continuity", Name: string(kind)})
	}
}

func traceStrategyChange(ctx context.Context) {
	traceFromContext(ctx).record(harnessmetrics.TraceEvent{Kind: "gate", Name: "strategy_monitor", Outcome: "send_back", Reason: "repeated_failure_or_observation"})
}

func traceRuntimeOutcome(ctx context.Context, name string, started time.Time, response *fantasy.ToolResponse, err *error) {
	outcome, reason := "pass", "dispatch_and_accounting_complete"
	if *err != nil || response.IsError {
		outcome, reason = "error", "tool_or_guard_failed"
	}
	if response.StopTurn {
		outcome, reason = "unverified", "tool_stopped_turn"
	}
	traceFromContext(ctx).record(harnessmetrics.TraceEvent{Kind: "gate", Name: "runtime_guard", CallID: name, Outcome: outcome, Reason: reason, DurationMS: time.Since(started).Milliseconds()})
}

func traceEvidenceOutcome(ctx context.Context, started time.Time, uncertain bool, err *error) {
	outcome, reason := "pass", "evidence_persisted"
	if uncertain {
		outcome, reason = "unverified", "effect_uncertain"
	}
	if *err != nil {
		outcome, reason = "error", "evidence_write_failed"
	}
	traceFromContext(ctx).record(harnessmetrics.TraceEvent{Kind: "gate", Name: "outcome_guard", Outcome: outcome, Reason: reason, DurationMS: time.Since(started).Milliseconds()})
}

// gateObservation owns helper usage, so nested reviews do not double charge.
type gateObservation struct {
	trace   *turnTrace
	name    string
	started time.Time
	mu      sync.Mutex
	usage   fantasy.Usage
	cost    float64
}
type gateKey struct{}

func startGate(ctx context.Context, name string) (context.Context, *gateObservation) {
	g := &gateObservation{trace: traceFromContext(ctx), name: name, started: time.Now()}
	return context.WithValue(ctx, gateKey{}, g), g
}

func (g *gateObservation) finish(outcome, reason string) {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.trace.record(harnessmetrics.TraceEvent{Kind: "gate", Name: g.name, Outcome: outcome, Reason: reason, DurationMS: time.Since(g.started).Milliseconds(), InputTokens: g.usage.InputTokens, OutputTokens: g.usage.OutputTokens, TotalTokens: g.usage.TotalTokens, CacheReadTokens: g.usage.CacheReadTokens, CacheWriteTokens: g.usage.CacheCreationTokens, Cost: g.cost})
}

func decisionOutcome(d evaluatorDecision, active bool) (string, string) {
	switch {
	case d.Continue != "":
		return "send_back", "required_work_remaining"
	case d.Unresolved != "":
		return "unresolved", "repair_budget_exhausted"
	case d.Unverified != "":
		return "unverified", string(d.Review)
	case !active:
		return "skipped", "not_required"
	default:
		return "pass", "checks_accepted"
	}
}

func (g *gateObservation) decision(d evaluatorDecision, active bool) {
	outcome, reason := decisionOutcome(d, active)
	g.finish(outcome, reason)
}

// observedTools retain tool metadata/provider options through embedding.
func observedTools(list []fantasy.AgentTool) []fantasy.AgentTool {
	out := make([]fantasy.AgentTool, len(list))
	for i, t := range list {
		out[i] = traceTool{AgentTool: t}
	}
	return out
}

type traceTool struct{ fantasy.AgentTool }

func (t traceTool) Run(ctx context.Context, call fantasy.ToolCall) (resp fantasy.ToolResponse, err error) {
	trace := traceFromContext(ctx)
	started := time.Now()
	defer func() {
		panicked := recover()
		outcome := "pass"
		if err != nil || resp.IsError {
			outcome = "error"
		}
		if resp.StopTurn {
			outcome = "stopped"
		}
		if panicked != nil {
			outcome = "panic"
		}
		if trace != nil {
			trace.tools.Store(call.ID, struct{}{})
		}
		trace.record(harnessmetrics.TraceEvent{Kind: "tool", Name: call.Name, CallID: call.ID, Outcome: outcome, Bytes: len(resp.Content), DurationMS: time.Since(started).Milliseconds()})
		if panicked != nil {
			panic(panicked)
		}
	}()
	return t.AgentTool.Run(ctx, call)
}

func (t *turnTrace) toolResult(result fantasy.ToolResultContent) {
	if t == nil {
		return
	}
	if _, recorded := t.tools.LoadOrStore(result.ToolCallID, struct{}{}); recorded {
		return
	}
	outcome := "pass"
	if _, failed := result.Result.(fantasy.ToolResultOutputContentError); failed {
		outcome = "error"
	}
	t.record(harnessmetrics.TraceEvent{Kind: "tool", Name: result.ToolName, CallID: result.ToolCallID, Outcome: outcome, Reason: "no_local_dispatch_duration_unmeasured", Bytes: len(toolResultOutputString(result.Result))})
}

// observedModel records each transport attempt without retaining stream text.
func observedModel(ctx context.Context, model Model) fantasy.LanguageModel {
	if traceFromContext(ctx) == nil {
		return model.Model
	}
	return &traceModel{LanguageModel: model.Model, model: model}
}

type traceModel struct {
	fantasy.LanguageModel
	model Model
}

func (m *traceModel) Stream(ctx context.Context, call fantasy.Call) (fantasy.StreamResponse, error) {
	started := time.Now()
	event := harnessmetrics.TraceEvent{Kind: "model", Name: m.Provider() + "/" + m.Model(), CallID: uuid.NewString(), Outcome: "interrupted", CostSource: "unavailable"}
	if g, ok := ctx.Value(gateKey{}).(*gateObservation); ok {
		event.Gate = g.name
	}
	traceFromContext(ctx).record(harnessmetrics.TraceEvent{Kind: "model_start", Name: event.Name, CallID: event.CallID, Gate: event.Gate})
	stream, err := m.LanguageModel.Stream(ctx, call)
	if err != nil {
		event.Outcome, event.Reason = "error", "transport_error"
		event.DurationMS = time.Since(started).Milliseconds()
		recordModelTrace(ctx, event)
		return nil, err
	}
	return func(yield func(fantasy.StreamPart) bool) {
		var usage fantasy.Usage
		outputBytes := 0
		defer func() {
			event.DurationMS = time.Since(started).Milliseconds()
			event.InputTokens, event.OutputTokens = usage.InputTokens, usage.OutputTokens
			event.TotalTokens = engineeringTokens(usage)
			event.CacheReadTokens, event.CacheWriteTokens = usage.CacheReadTokens, usage.CacheCreationTokens
			recordModelTrace(ctx, event)
		}()
		for part := range stream {
			outputBytes += len(part.Delta) + len(part.ToolCallInput) + len(part.ToolCallName)
			if part.Type == fantasy.StreamPartTypeFinish {
				usage = part.Usage
				event.Estimated = usageIsZero(usage)
				if event.Estimated {
					usage.InputTokens = estimateMessageTokens(call.Prompt)
					for _, tool := range call.Tools {
						data, err := json.Marshal(tool)
						if err == nil {
							usage.InputTokens += approxTokenCount(string(data))
						}
					}
					usage.OutputTokens = int64((outputBytes + 3) / 4)
				}
				event.Cost, event.CostSource = modelTraceCost(m.model, usage, part.ProviderMetadata)
				event.Outcome, event.Reason = "pass", string(part.FinishReason)
			}
			if part.Type == fantasy.StreamPartTypeError {
				event.Outcome, event.Reason = "error", "stream_error"
			}
			if !yield(part) {
				return
			}
		}
	}, nil
}

func modelTraceCost(model Model, u fantasy.Usage, metadata fantasy.ProviderMetadata) (float64, string) {
	if model.FlatRate {
		return 0, "flat_rate"
	}
	if data, ok := metadata[openrouter.Name].(*openrouter.ProviderMetadata); ok {
		return max(0, data.Usage.Cost), "provider_reported"
	}
	p := model.CatwalkCfg
	if u.TotalTokens > 0 && u.InputTokens == 0 && u.OutputTokens == 0 && u.CacheReadTokens == 0 && u.CacheCreationTokens == 0 {
		return 0, "unavailable"
	}
	return max(0, (p.CostPer1MIn*float64(u.InputTokens)+p.CostPer1MOut*float64(u.OutputTokens)+p.CostPer1MInCached*float64(u.CacheCreationTokens)+p.CostPer1MOutCached*float64(u.CacheReadTokens))/1e6), "catalog_estimate"
}

func recordModelTrace(ctx context.Context, event harnessmetrics.TraceEvent) {
	traceFromContext(ctx).record(event)
	if g, ok := ctx.Value(gateKey{}).(*gateObservation); ok {
		g.mu.Lock()
		defer g.mu.Unlock()
		g.usage.InputTokens += event.InputTokens
		g.usage.TotalTokens += event.Tokens()
		g.usage.OutputTokens += event.OutputTokens
		g.usage.CacheReadTokens += event.CacheReadTokens
		g.usage.CacheCreationTokens += event.CacheWriteTokens
		g.cost += event.Cost
	}
}
