package agent

import (
	"context"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
)

// attemptModel adapts each transport attempt, including retries that switch
// provider without rerunning PrepareStep. The embedded client owns auth.
type attemptModel struct {
	fantasy.LanguageModel
	prepare    func(fantasy.Call) fantasy.Call
	beforeCall func(context.Context) error
}

func (m *attemptModel) Generate(ctx context.Context, call fantasy.Call) (*fantasy.Response, error) {
	if m.beforeCall != nil {
		if err := m.beforeCall(ctx); err != nil {
			return nil, err
		}
	}
	return m.LanguageModel.Generate(ctx, m.prepare(call))
}

func (m *attemptModel) Stream(ctx context.Context, call fantasy.Call) (fantasy.StreamResponse, error) {
	if m.beforeCall != nil {
		if err := m.beforeCall(ctx); err != nil {
			return nil, err
		}
	}
	return m.LanguageModel.Stream(ctx, m.prepare(call))
}

func (a *sessionAgent) prepareAttempt(call fantasy.Call, model Model, initial reasoningOrigin, origins map[string]reasoningOrigin) fantasy.Call {
	call.Prompt = filterTurnReasoning(call.Prompt, origins, originOf(model))
	call.Prompt = a.workaroundProviderMediaLimitations(call.Prompt, model)
	if originOf(model) != initial {
		// Options produced for the original adapter must not leak to the
		// fallback adapter. Keep caller settings on the original path.
		call.ProviderOptions = nil
		budget := model.CatwalkCfg.DefaultMaxTokens
		if model.ModelCfg.MaxTokens > 0 {
			budget = model.ModelCfg.MaxTokens
		}
		if budget > 0 && call.MaxOutputTokens != nil && *call.MaxOutputTokens > budget {
			call.MaxOutputTokens = &budget
		}
		if a.taskContextConfig != nil {
			if provider, ok := a.taskContextConfig.Config().Providers.Get(model.ModelCfg.Provider); ok {
				call.ProviderOptions, _ = resolveProviderOptions(model, provider)
			}
		}
	}
	return call
}
