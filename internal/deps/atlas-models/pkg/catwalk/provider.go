package catwalk

// Type represents the type of AI provider.
type Type string

// All the supported AI provider types.
const (
	TypeOpenAI       Type = "openai"
	TypeOpenAICompat Type = "openai-compat"
	TypeOpenRouter   Type = "openrouter"
	TypeVercel       Type = "vercel"
	TypeAnthropic    Type = "anthropic"
	TypeGoogle       Type = "google"
	TypeAzure        Type = "azure"
	TypeBedrock      Type = "bedrock"
	TypeVertexAI     Type = "google-vertex"
	TypeAntigravity  Type = "antigravity"
	TypeClaude       Type = "claude"
	TypeGrokWeb      Type = "grok-web"
	TypeWindsurf     Type = "windsurf"
	TypeJetBrains    Type = "jetbrains"
	TypeAugment      Type = "augment"
	TypeFactory      Type = "factory"
	TypeCodeRabbit   Type = "coderabbit"
	TypeZed          Type = "zed"
	TypeMuse         Type = "muse"
)

// Coding-plan provider types: these are the custom protocol adapters
// Atlas-Agent ships for subscription plans whose public API is either
// not documented (claude.ai console, grok.com, codeium-backed
// Windsurf) or routed through a JWT exchange (JetBrains AI). Each one
// is implemented as its own fantasy.Provider under
// internal/deps/atlas-llm/providers/<name> and wired into the
// coordinator by Type. Adding a new coding plan = (1) add a Type
// constant, (2) implement a provider package, (3) add a case to
// buildProvider in coordinator.go, (4) add a login subcommand.

// InferenceProvider represents the inference provider identifier.
type InferenceProvider string

// All the inference providers supported by the system.
const (
	InferenceProviderOpenAI           InferenceProvider = "openai"
	InferenceProviderAnthropic        InferenceProvider = "anthropic"
	InferenceProviderSynthetic        InferenceProvider = "synthetic"
	InferenceProviderGemini           InferenceProvider = "gemini"
	InferenceProviderAzure            InferenceProvider = "azure"
	InferenceProviderBedrock          InferenceProvider = "bedrock"
	InferenceProviderBedrockEurope    InferenceProvider = "bedrock-europe"
	InferenceProviderVertexAI         InferenceProvider = "vertexai"
	InferenceProviderXAI              InferenceProvider = "xai"
	InferenceProviderZAI              InferenceProvider = "zai"
	InferenceProviderDeepSeek         InferenceProvider = "deepseek"
	InferenceProviderZhipu            InferenceProvider = "zhipu"
	InferenceProviderZhipuCoding      InferenceProvider = "zhipu-coding"
	InferenceProviderGROQ             InferenceProvider = "groq"
	InferenceProviderOpenRouter       InferenceProvider = "openrouter"
	InferenceProviderCerebras         InferenceProvider = "cerebras"
	InferenceProviderVenice           InferenceProvider = "venice"
	InferenceProviderChutes           InferenceProvider = "chutes"
	InferenceProviderHuggingFace      InferenceProvider = "huggingface"
	InferenceAIHubMix                 InferenceProvider = "aihubmix"
	InferenceKimiCoding               InferenceProvider = "kimi-coding"
	InferenceProviderCopilot          InferenceProvider = "copilot"
	InferenceProviderChatGPT          InferenceProvider = "chatgpt"
	InferenceProviderAntigravity      InferenceProvider = "antigravity"
	InferenceProviderClaude           InferenceProvider = "claude"
	InferenceProviderGrokWeb          InferenceProvider = "grok-web"
	InferenceProviderWindsurf         InferenceProvider = "windsurf"
	InferenceProviderJetBrains        InferenceProvider = "jetbrains"
	InferenceProviderAugment          InferenceProvider = "augment"
	InferenceProviderFactory          InferenceProvider = "factory"
	InferenceProviderCodeRabbit       InferenceProvider = "coderabbit"
	InferenceProviderZed              InferenceProvider = "zed"
	InferenceProviderMuse             InferenceProvider = "muse"
	InferenceProviderCortecs          InferenceProvider = "cortecs"
	InferenceProviderVercel           InferenceProvider = "vercel"
	InferenceProviderMiniMax          InferenceProvider = "minimax"
	InferenceProviderMiniMaxChina     InferenceProvider = "minimax-china"
	InferenceProviderIoNet            InferenceProvider = "ionet"
	InferenceProviderQiniuCloud       InferenceProvider = "qiniucloud"
	InferenceProviderAvian            InferenceProvider = "avian"
	InferenceProviderNebius           InferenceProvider = "nebius"
	InferenceProviderNeuralwatt       InferenceProvider = "neuralwatt"
	InferenceProviderOpenCodeZen      InferenceProvider = "opencode-zen"
	InferenceProviderOpenCodeGo       InferenceProvider = "opencode-go"
	InferenceProviderAlibabaSingapore InferenceProvider = "alibaba-singapore"
	InferenceProviderAlibabaUS        InferenceProvider = "alibaba-us"
	InferenceProviderFireworks        InferenceProvider = "fireworks"
	InferenceProviderBaseten          InferenceProvider = "baseten"
	InferenceProviderMoonshot         InferenceProvider = "moonshot"
	InferenceProviderAtlasCloud       InferenceProvider = "atlascloud"
	InferenceProviderNvidiaNIM        InferenceProvider = "nvidia-nim"
)

// Provider represents an AI provider configuration.
type Provider struct {
	Name                string            `json:"name"`
	ID                  InferenceProvider `json:"id"`
	APIKey              string            `json:"api_key,omitempty"`
	APIEndpoint         string            `json:"api_endpoint,omitempty"`
	Type                Type              `json:"type,omitempty"`
	DefaultLargeModelID string            `json:"default_large_model_id,omitempty"`
	DefaultSmallModelID string            `json:"default_small_model_id,omitempty"`
	Models              []Model           `json:"models,omitempty"`
	DefaultHeaders      map[string]string `json:"default_headers,omitempty"`
}

// ModelOptions stores extra options for models.
type ModelOptions struct {
	Temperature      *float64       `json:"temperature,omitempty"`
	TopP             *float64       `json:"top_p,omitempty"`
	TopK             *int64         `json:"top_k,omitempty"`
	FrequencyPenalty *float64       `json:"frequency_penalty,omitempty"`
	PresencePenalty  *float64       `json:"presence_penalty,omitempty"`
	ProviderOptions  map[string]any `json:"provider_options,omitempty"`
}

// Model represents an AI model configuration.
type Model struct {
	ID                     string       `json:"id"`
	Name                   string       `json:"name"`
	CostPer1MIn            float64      `json:"cost_per_1m_in"`
	CostPer1MOut           float64      `json:"cost_per_1m_out"`
	CostPer1MInCached      float64      `json:"cost_per_1m_in_cached"`
	CostPer1MOutCached     float64      `json:"cost_per_1m_out_cached"`
	ContextWindow          int64        `json:"context_window"`
	DefaultMaxTokens       int64        `json:"default_max_tokens"`
	CanReason              bool         `json:"can_reason"`
	ReasoningLevels        []string     `json:"reasoning_levels,omitempty"`
	DefaultReasoningEffort string       `json:"default_reasoning_effort,omitempty"`
	SupportsImages         bool         `json:"supports_attachments"`
	Options                ModelOptions `json:"options,omitzero"`
}

// KnownProviders returns all the known inference providers.
func KnownProviders() []InferenceProvider {
	return []InferenceProvider{
		InferenceProviderOpenAI,
		InferenceProviderSynthetic,
		InferenceProviderAnthropic,
		InferenceProviderGemini,
		InferenceProviderAzure,
		InferenceProviderBedrock,
		InferenceProviderBedrockEurope,
		InferenceProviderVertexAI,
		InferenceProviderXAI,
		InferenceProviderZAI,
		InferenceProviderZhipu,
		InferenceProviderZhipuCoding,
		InferenceProviderGROQ,
		InferenceProviderOpenRouter,
		InferenceProviderCerebras,
		InferenceProviderVenice,
		InferenceProviderChutes,
		InferenceProviderHuggingFace,
		InferenceAIHubMix,
		InferenceKimiCoding,
		InferenceProviderCopilot,
		InferenceProviderChatGPT,
		InferenceProviderCortecs,
		InferenceProviderVercel,
		InferenceProviderMiniMax,
		InferenceProviderMiniMaxChina,
		InferenceProviderQiniuCloud,
		InferenceProviderAvian,
		InferenceProviderNebius,
		InferenceProviderNeuralwatt,
		InferenceProviderOpenCodeZen,
		InferenceProviderOpenCodeGo,
		InferenceProviderFireworks,
		InferenceProviderBaseten,
		InferenceProviderMoonshot,
		InferenceProviderAtlasCloud,
		InferenceProviderNvidiaNIM,
	}
}

// KnownProviderTypes returns all the known inference providers types.
func KnownProviderTypes() []Type {
	return []Type{
		TypeOpenAI,
		TypeOpenAICompat,
		TypeOpenRouter,
		TypeVercel,
		TypeAnthropic,
		TypeGoogle,
		TypeAzure,
		TypeBedrock,
		TypeVertexAI,
		TypeAntigravity,
		TypeClaude,
		TypeGrokWeb,
		TypeWindsurf,
		TypeJetBrains,
		TypeAugment,
		TypeFactory,
		TypeCodeRabbit,
		TypeZed,
		TypeMuse,
	}
}
