package agent

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/notify"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/pubsub"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm/providers/anthropic"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm/providers/antigravity"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm/providers/azure"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm/providers/bedrock"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm/providers/claude"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm/providers/google"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm/providers/muse"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm/providers/openai"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm/providers/openaicompat"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm/providers/openrouter"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm/providers/vercel"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-models/pkg/catwalk"
)

// CapabilityAdjustment records a configured setting that the selected
// provider path cannot apply as requested, and what is sent instead.
type CapabilityAdjustment struct {
	Setting   string `json:"setting"`
	Requested string `json:"requested,omitempty"`
	Applied   string `json:"applied"`
	Reason    string `json:"reason"`
}

func (a CapabilityAdjustment) String() string {
	if a.Requested == "" {
		return fmt.Sprintf("%s: %s (%s)", a.Setting, a.Applied, a.Reason)
	}
	return fmt.Sprintf("%s %q -> %s (%s)", a.Setting, a.Requested, a.Applied, a.Reason)
}

// ProviderCapabilities describes what Atlas actually sends for a model on a
// provider path. It is derived from the catalog entry, the user's model
// settings and the adapter the request goes through, never from the model's
// name alone.
type ProviderCapabilities struct {
	Basis            string                 `json:"basis"`
	Provider         string                 `json:"provider"`
	Model            string                 `json:"model"`
	Adapter          string                 `json:"adapter"`
	CanReason        bool                   `json:"can_reason"`
	ReasoningLevels  []string               `json:"reasoning_levels,omitempty"`
	RequestedEffort  string                 `json:"requested_effort,omitempty"`
	EffectiveEffort  string                 `json:"effective_effort,omitempty"`
	ReasoningReplay  string                 `json:"reasoning_replay"`
	Images           bool                   `json:"images"`
	ToolCalling      string                 `json:"tool_calling"`
	ParallelTools    string                 `json:"parallel_tool_calls"`
	StructuredOutput string                 `json:"structured_output"`
	PromptCache      string                 `json:"prompt_cache"`
	ContextWindow    int64                  `json:"context_window"`
	DefaultMaxTokens int64                  `json:"default_max_tokens"`
	MaxOutputTokens  int64                  `json:"max_output_tokens"`
	Adjustments      []CapabilityAdjustment `json:"adjustments,omitempty"`
}

// adapterType returns the adapter a request for this model is routed
// through. Some providers serve several wire protocols.
func adapterType(model Model, providerCfg config.ProviderConfig) catwalk.Type {
	t := providerCfg.Type
	if t == catwalk.TypeVertexAI {
		if strings.HasPrefix(model.CatwalkCfg.ID, "claude-") {
			return catwalk.TypeAnthropic
		}
		return catwalk.TypeGoogle
	}
	if providerCfg.ID == string(catwalk.InferenceProviderOpenCodeGo) || providerCfg.ID == string(catwalk.InferenceProviderOpenCodeZen) {
		return opencodeModelType(providerCfg.ID, model.CatwalkCfg.ID)
	}
	if providerCfg.ID == string(catwalk.InferenceProviderCopilot) && copilotResponsesModels[model.CatwalkCfg.ID] {
		return catwalk.TypeOpenAI
	}
	return t
}

// reasoningAdjustments reports reasoning settings that differ from what the
// catalog says the model accepts.
func reasoningAdjustments(model Model) []CapabilityAdjustment {
	var out []CapabilityAdjustment
	requested := model.ModelCfg.ReasoningEffort
	levels := model.CatwalkCfg.ReasoningLevels
	switch {
	case requested != "" && !model.CatwalkCfg.CanReason:
		out = append(out, CapabilityAdjustment{Setting: "reasoning_effort", Requested: requested, Applied: "none", Reason: "the catalog entry for this model does not support reasoning"})
	case requested != "" && len(levels) == 0:
		out = append(out, CapabilityAdjustment{Setting: "reasoning_effort", Requested: requested, Applied: "provider default", Reason: "the catalog lists no reasoning levels for this model, so no effort is sent"})
	case requested != "" && !slices.Contains(levels, requested):
		out = append(out, CapabilityAdjustment{Setting: "reasoning_effort", Requested: requested, Applied: cmpOrNone(effectiveReasoningEffort(model)), Reason: "not one of the model's levels: " + strings.Join(levels, ", ")})
	}
	if model.ModelCfg.Think && !model.CatwalkCfg.CanReason {
		out = append(out, CapabilityAdjustment{Setting: "think", Requested: "true", Applied: "none", Reason: "the catalog entry for this model does not support reasoning"})
	}
	return out
}

func cmpOrNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}

// reasoningReplay describes which reasoning state is carried across turns
// for the adapter. Every kind is replayed only to the model that produced it.
func reasoningReplay(adapter catwalk.Type, model Model) string {
	if !model.CatwalkCfg.CanReason {
		return "none"
	}
	switch adapter {
	case anthropic.Name, bedrock.Name, claude.Name, muse.Name:
		return "signed thinking blocks, same model only"
	case openai.Name, azure.Name:
		if openai.IsResponsesReasoningModel(model.CatwalkCfg.ID) {
			return "encrypted reasoning items, same model only"
		}
		return "none"
	case google.Name, antigravity.Name:
		return "thought signatures, same model only"
	case openaicompat.Name, openrouter.Name, vercel.Name:
		return "reasoning text, same model only"
	default:
		return "none"
	}
}

// ResolveCapabilities reports what is sent for a model on its provider path.
func ResolveCapabilities(model Model, providerCfg config.ProviderConfig) ProviderCapabilities {
	adapter := adapterType(model, providerCfg)
	options, adjustments := resolveProviderOptions(model, providerCfg)
	effort := appliedEffort(options)
	if requested := model.ModelCfg.ReasoningEffort; requested != "" && effort != requested && len(adjustments) == 0 {
		adjustments = append(adjustments, CapabilityAdjustment{Setting: "reasoning_effort", Requested: requested, Applied: cmpOrNone(effort), Reason: "the parsed provider options override the selected effort or use a budget instead of an effort level"})
	}
	maxOut := model.CatwalkCfg.DefaultMaxTokens
	if model.ModelCfg.MaxTokens != 0 {
		maxOut = model.ModelCfg.MaxTokens
	}
	if cw := model.CatwalkCfg.ContextWindow; cw > 0 && maxOut > cw {
		adjustments = append(adjustments, CapabilityAdjustment{Setting: "max_tokens", Requested: fmt.Sprint(maxOut), Applied: fmt.Sprint(maxOut), Reason: "exceeds the catalog context window; the provider may reject or truncate it"})
	}
	caps := ProviderCapabilities{
		Basis:            "catalog and locally parsed adapter options; upstream support is not live-verified",
		Provider:         model.ModelCfg.Provider,
		Model:            model.ModelCfg.Model,
		Adapter:          string(adapter),
		CanReason:        model.CatwalkCfg.CanReason,
		ReasoningLevels:  model.CatwalkCfg.ReasoningLevels,
		RequestedEffort:  model.ModelCfg.ReasoningEffort,
		EffectiveEffort:  effort,
		ReasoningReplay:  reasoningReplay(adapter, model),
		Images:           model.CatwalkCfg.SupportsImages,
		ToolCalling:      "function tools; schemas validated by the adapter",
		ParallelTools:    "provider default; not configured by Atlas",
		StructuredOutput: "adapter GenerateObject/StreamObject contract; normal agent turns use tool schemas and do not request a response schema",
		PromptCache:      "none",
		ContextWindow:    model.CatwalkCfg.ContextWindow,
		DefaultMaxTokens: model.CatwalkCfg.DefaultMaxTokens,
		MaxOutputTokens:  maxOut,
		Adjustments:      adjustments,
	}
	switch adapter {
	case anthropic.Name, bedrock.Name, claude.Name, muse.Name:
		caps.PromptCache = "explicit cache_control breakpoints (system, last tool, last messages)"
	case openai.Name, azure.Name, openaicompat.Name, openrouter.Name, vercel.Name, google.Name, antigravity.Name:
		caps.PromptCache = "provider automatic prefix caching, if offered"
	}
	if _, ok := mergedProviderOption(model, providerCfg, "parallel_tool_calls"); ok {
		caps.ParallelTools = "configured in provider_options"
	}
	return caps
}

// appliedEffort reads the resolved adapter options instead of repeating
// the catalog default. Explicit provider_options can override that default.
func appliedEffort(options fantasy.ProviderOptions) string {
	for _, option := range options {
		data, err := json.Marshal(option)
		if err != nil {
			continue
		}
		var wrapper struct {
			Data json.RawMessage `json:"data"`
		}
		if json.Unmarshal(data, &wrapper) == nil && len(wrapper.Data) > 0 {
			data = wrapper.Data
		}
		var value struct {
			Effort          string          `json:"effort"`
			ReasoningEffort string          `json:"reasoning_effort"`
			Reasoning       json.RawMessage `json:"reasoning"`
			Thinking        *struct {
				Level string `json:"thinking_level"`
			} `json:"thinking_config"`
			ExtraBody *struct {
				Effort string `json:"reasoning_effort"`
			} `json:"extra_body"`
		}
		if json.Unmarshal(data, &value) != nil {
			continue
		}
		if value.Effort != "" {
			return value.Effort
		}
		if value.ReasoningEffort != "" {
			return value.ReasoningEffort
		}
		var reasoning struct {
			Effort string `json:"effort"`
		}
		if json.Unmarshal(value.Reasoning, &reasoning) == nil && reasoning.Effort != "" {
			return reasoning.Effort
		}
		if value.Thinking != nil {
			return value.Thinking.Level
		}
		if value.ExtraBody != nil {
			return value.ExtraBody.Effort
		}
	}
	return ""
}

func mergedProviderOption(model Model, providerCfg config.ProviderConfig, key string) (any, bool) {
	for _, m := range []map[string]any{model.ModelCfg.ProviderOptions, providerCfg.ProviderOptions, model.CatwalkCfg.Options.ProviderOptions} {
		if v, ok := m[key]; ok {
			return v, true
		}
	}
	return nil, false
}

// reportedAdjustments deduplicates capability notices per process, so a
// setting that cannot be applied is reported once rather than every turn.
var reportedAdjustments sync.Map

// reportCapabilityAdjustments logs and surfaces settings the provider path
// cannot apply as configured.
func (c *coordinator) reportCapabilityAdjustments(sessionID string, model Model, providerCfg config.ProviderConfig) {
	adjustments := ResolveCapabilities(model, providerCfg).Adjustments
	for _, adj := range adjustments {
		key := sessionID + "\x00" + model.ModelCfg.Provider + "\x00" + model.ModelCfg.Model + "\x00" + adj.String()
		if _, seen := reportedAdjustments.LoadOrStore(key, true); seen {
			continue
		}
		slog.Warn("Model setting not applied as configured",
			"provider", model.ModelCfg.Provider, "model", model.ModelCfg.Model,
			"setting", adj.Setting, "requested", adj.Requested, "applied", adj.Applied, "reason", adj.Reason)
		if c.notify != nil {
			c.notify.Publish(pubsub.CreatedEvent, notify.Notification{
				SessionID:  sessionID,
				Type:       notify.TypeCapabilityNotice,
				ProviderID: model.ModelCfg.Provider,
				Message:    fmt.Sprintf("%s/%s: %s", model.ModelCfg.Provider, model.ModelCfg.Model, adj),
			})
		}
	}
}
