package agent

import (
	"fmt"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/event"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/harnessmetrics"
)

func (a *sessionAgent) eventPromptSent(sessionID string) {
	event.PromptSent(
		a.eventCommon(sessionID, a.largeModel.Get())...,
	)
}

func (a *sessionAgent) eventPromptResponded(sessionID string, duration time.Duration) {
	event.PromptResponded(
		append(
			a.eventCommon(sessionID, a.largeModel.Get()),
			"prompt duration pretty", duration.String(),
			"prompt duration in seconds", int64(duration.Seconds()),
		)...,
	)
}

func (a *sessionAgent) eventTokensUsed(sessionID string, model Model, usage fantasy.Usage, cost float64) {
	cacheInputs := usage.InputTokens + usage.CacheReadTokens
	hitRate := 0.0
	if cacheInputs > 0 {
		hitRate = float64(usage.CacheReadTokens) / float64(cacheInputs)
	}
	a.recordHarness(harnessmetrics.PromptCache, sessionID, map[string]string{
		"cache_read_tokens":     fmt.Sprint(usage.CacheReadTokens),
		"cache_creation_tokens": fmt.Sprint(usage.CacheCreationTokens),
		"cache_hit_rate":        fmt.Sprintf("%.4f", hitRate),
	})
	event.TokensUsed(
		append(
			a.eventCommon(sessionID, model),
			"input tokens", usage.InputTokens,
			"output tokens", usage.OutputTokens,
			"cache read tokens", usage.CacheReadTokens,
			"cache creation tokens", usage.CacheCreationTokens,
			"total tokens", usage.InputTokens+usage.OutputTokens+usage.CacheReadTokens+usage.CacheCreationTokens,
			"cost", cost,
		)...,
	)
}

func (a *sessionAgent) eventCommon(sessionID string, model Model) []any {
	m := model.ModelCfg

	return []any{
		"session id", sessionID,
		"provider", m.Provider,
		"model", m.Model,
		"reasoning effort", m.ReasoningEffort,
		"thinking mode", m.Think,
		"yolo mode", a.isYolo,
	}
}
