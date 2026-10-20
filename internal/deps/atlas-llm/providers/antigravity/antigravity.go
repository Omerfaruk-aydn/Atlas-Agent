// Package antigravity implements a fantasy.Provider for Google's
// Antigravity (Cloud Code) backend: the same account a signed-in
// Antigravity IDE uses, reached through its internal
// "v1internal:generateContent"/"streamGenerateContent" endpoints rather
// than the public Gemini API or Vertex AI, both of which use a different
// request envelope.
//
// The account endpoint accepts the Gemini-shaped envelope for Gemini,
// Claude and GPT-OSS. Model-family translation happens on the server.
// Selectable model IDs are resolved to the account's effort-specific
// routes rather than sending public-provider IDs or thinking settings.
package antigravity

import (
	"bufio"
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm/object"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm/providers/google"
	"github.com/google/uuid"
)

// Name is the name of the Antigravity provider, matched against
// catwalk.Type by the coordinator's provider dispatch.
const Name = "antigravity"

const defaultBaseURL = "https://daily-cloudcode-pa.googleapis.com"

type options struct {
	apiKey  string
	baseURL string
	project string
	headers map[string]string
	client  *http.Client
}

// Option configures the Antigravity provider.
type Option = func(*options)

// WithAPIKey sets the bearer token (the account's OAuth access token).
func WithAPIKey(apiKey string) Option {
	return func(o *options) { o.apiKey = apiKey }
}

// WithBaseURL overrides the Cloud Code endpoint. Empty keeps the default.
func WithBaseURL(baseURL string) Option {
	return func(o *options) { o.baseURL = baseURL }
}

// WithProject sets the Cloud project id discovered/provisioned at login
// (see internal/oauth/antigravity), required on every request.
func WithProject(project string) Option {
	return func(o *options) { o.project = project }
}

// WithHeaders sets additional headers (e.g. Client-Metadata, User-Agent).
func WithHeaders(headers map[string]string) Option {
	return func(o *options) {
		if o.headers == nil {
			o.headers = map[string]string{}
		}
		for k, v := range headers {
			o.headers[k] = v
		}
	}
}

// WithHTTPClient sets the HTTP client used for requests.
func WithHTTPClient(client *http.Client) Option {
	return func(o *options) { o.client = client }
}

type provider struct {
	options options
}

// New creates a new Antigravity provider.
func New(opts ...Option) (fantasy.Provider, error) {
	o := options{headers: map[string]string{}, client: http.DefaultClient}
	for _, opt := range opts {
		opt(&o)
	}
	o.baseURL = cmp.Or(o.baseURL, defaultBaseURL)
	if o.apiKey == "" {
		return nil, errors.New("antigravity: missing access token")
	}
	if o.project == "" {
		return nil, errors.New("antigravity: missing Cloud project id; sign in again with `atlas login antigravity`")
	}
	return &provider{options: o}, nil
}

func (*provider) Name() string { return Name }

type languageModel struct {
	modelID string
	options options
}

// LanguageModel implements fantasy.Provider.
func (p *provider) LanguageModel(_ context.Context, modelID string) (fantasy.LanguageModel, error) {
	return &languageModel{modelID: modelID, options: p.options}, nil
}

func (m *languageModel) Model() string    { return m.modelID }
func (m *languageModel) Provider() string { return Name }

// -- Gemini-shaped request/response envelope --------------------------------

type geminiPart struct {
	ThoughtSignature []byte            `json:"thoughtSignature,omitempty"`
	InlineData       *geminiInlineData `json:"inlineData,omitempty"`
	Text             string            `json:"text,omitempty"`
	Thought          bool              `json:"thought,omitempty"`
	FunctionCall     *geminiFuncCall   `json:"functionCall,omitempty"`
	FunctionResponse *geminiFuncResult `json:"functionResponse,omitempty"`
}

type geminiInlineData struct {
	MIMEType string `json:"mimeType"`
	Data     []byte `json:"data"`
}

type geminiFuncCall struct {
	ID   string         `json:"id,omitempty"`
	Name string         `json:"name"`
	Args map[string]any `json:"args,omitempty"`
}

type geminiFuncResult struct {
	ID       string         `json:"id,omitempty"`
	Name     string         `json:"name"`
	Response map[string]any `json:"response,omitempty"`
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type generationConfig struct {
	Temperature     *float32        `json:"temperature,omitempty"`
	TopP            *float32        `json:"topP,omitempty"`
	TopK            *float32        `json:"topK,omitempty"`
	MaxOutputTokens int32           `json:"maxOutputTokens,omitempty"`
	ThinkingConfig  *thinkingConfig `json:"thinkingConfig,omitempty"`
}

// thinkingConfig controls Gemini 3's thinking_level knob (low/medium/high),
// which replaced the older thinking_budget field. See ProviderOptions.
type thinkingConfig struct {
	ThinkingLevel   string `json:"thinkingLevel,omitempty"`
	IncludeThoughts bool   `json:"includeThoughts,omitempty"`
}

// ProviderOptions carries Antigravity-specific per-call options, set via
// fantasy.Call.ProviderOptions[Name]. Mirrors the shape the coordinator
// already builds for the "google" provider (see getProviderOptions in
// internal/agent/coordinator.go), so a model's reasoning-effort setting
// reaches the request the same way.
type ProviderOptions struct {
	ThinkingLevel string `json:"thinking_level,omitempty"`
}

// Options implements fantasy.ProviderOptionsData.
func (o *ProviderOptions) Options() {}

// MarshalJSON implements json.Marshaler for fantasy.ProviderOptionsData.
func (o ProviderOptions) MarshalJSON() ([]byte, error) {
	type plain ProviderOptions
	return json.Marshal(plain(o))
}

// UnmarshalJSON implements json.Unmarshaler for fantasy.ProviderOptionsData.
func (o *ProviderOptions) UnmarshalJSON(data []byte) error {
	type plain ProviderOptions
	var p plain
	if err := json.Unmarshal(data, &p); err != nil {
		return err
	}
	*o = ProviderOptions(p)
	return nil
}

// ParseOptions parses provider options from a merged options map for the
// Antigravity provider.
func ParseOptions(data map[string]any) (*ProviderOptions, error) {
	var options ProviderOptions
	if err := fantasy.ParseOptions(data, &options); err != nil {
		return nil, err
	}
	return &options, nil
}

type functionDeclaration struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

type geminiTool struct {
	FunctionDeclarations []functionDeclaration `json:"functionDeclarations,omitempty"`
}

type generateContentRequest struct {
	Contents          []geminiContent   `json:"contents"`
	SystemInstruction *geminiContent    `json:"systemInstruction,omitempty"`
	GenerationConfig  *generationConfig `json:"generationConfig,omitempty"`
	Tools             []geminiTool      `json:"tools,omitempty"`
}

// envelope wraps a Gemini-shaped request the way Antigravity's Cloud Code
// backend expects it, distinct from both the public Gemini API and Vertex
// AI: project, model and a request id sit alongside the request body
// instead of being encoded in the URL path.
type envelope struct {
	Project     string                 `json:"project"`
	Model       string                 `json:"model"`
	Request     generateContentRequest `json:"request"`
	UserAgent   string                 `json:"userAgent"`
	RequestID   string                 `json:"requestId"`
	RequestType string                 `json:"requestType"`
}

type usageMetadata struct {
	PromptTokenCount        int `json:"promptTokenCount"`
	CandidatesTokenCount    int `json:"candidatesTokenCount"`
	TotalTokenCount         int `json:"totalTokenCount"`
	ThoughtsTokenCount      int `json:"thoughtsTokenCount"`
	CachedContentTokenCount int `json:"cachedContentTokenCount"`
}

type responseCandidate struct {
	Content      geminiContent `json:"content"`
	FinishReason string        `json:"finishReason"`
}

type generateContentResponse struct {
	Candidates    []responseCandidate `json:"candidates"`
	UsageMetadata *usageMetadata      `json:"usageMetadata"`
	Error         *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (r *generateContentResponse) UnmarshalJSON(data []byte) error {
	type plain generateContentResponse
	var wrapped struct {
		Response json.RawMessage `json:"response"`
	}
	if err := json.Unmarshal(data, &wrapped); err != nil {
		return err
	}
	if len(wrapped.Response) > 0 && string(wrapped.Response) != "null" {
		data = wrapped.Response
	}
	var decoded plain
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*r = generateContentResponse(decoded)
	return nil
}

func resolveModel(modelID, effort string) (string, bool, error) {
	// Explicit account routes may appear in custom model configurations.
	// Honor their suffix by default and keep selected effort changes routed.
	for _, level := range []string{"low", "medium", "high"} {
		base, found := strings.CutSuffix(modelID, "-"+level)
		if !found {
			continue
		}
		switch base {
		case "gemini-4-argon", "gemini-3.8-flash", "gemini-3.7-flash", "gemini-3.6-flash", "gemini-3.1-pro",
			"claude-sonnet-5-5", "claude-opus-5-5", "gpt-oss-120b":
			modelID, effort = base, cmp.Or(effort, level)
		}
		break
	}
	switch modelID {
	case "gemini-4-argon", "gemini-3.8-flash", "gemini-3.7-flash", "gemini-3.6-flash":
		effort = cmp.Or(effort, "medium")
		if effort != "low" && effort != "medium" && effort != "high" {
			return "", true, fmt.Errorf("antigravity: %s supports low, medium or high effort", modelID)
		}
	case "gemini-3.1-pro":
		effort = cmp.Or(effort, "high")
		if effort != "low" && effort != "high" {
			return "", true, errors.New("antigravity: Gemini 3.1 Pro supports low or high effort")
		}
	case "claude-sonnet-5-5", "claude-opus-5-5":
		effort = cmp.Or(effort, "medium")
		if effort != "low" && effort != "medium" && effort != "high" {
			return "", true, fmt.Errorf("antigravity: %s supports low, medium or high effort", modelID)
		}
	case "gpt-oss-120b":
		effort = cmp.Or(effort, "medium")
		if effort != "medium" {
			return "", true, errors.New("antigravity: GPT-OSS 120B supports only medium effort")
		}
	default:
		return modelID, false, nil
	}
	return modelID + "-" + effort, true, nil
}

// -- request building ---------------------------------------------------

func (m *languageModel) buildEnvelope(call fantasy.Call) (envelope, []fantasy.CallWarning, error) {
	var effort string
	if opts, ok := call.ProviderOptions[Name].(*ProviderOptions); ok {
		effort = opts.ThinkingLevel
	}
	modelID, routed, err := resolveModel(m.modelID, effort)
	if err != nil {
		return envelope{}, nil, err
	}
	// Account limits come from fetchAvailableModels, not the public APIs.
	// Reject unsupported input locally instead of issuing a doomed request.
	var maxTokens int64
	switch modelID {
	case "claude-sonnet-5-5-low", "claude-sonnet-5-5-medium", "claude-sonnet-5-5-high",
		"claude-opus-5-5-low", "claude-opus-5-5-medium", "claude-opus-5-5-high":
		maxTokens = 128000
	case "gpt-oss-120b-medium":
		maxTokens = 32768
		for _, msg := range call.Prompt {
			for _, part := range msg.Content {
				if _, ok := fantasy.AsMessagePart[fantasy.FilePart](part); ok {
					return envelope{}, nil, errors.New("antigravity: GPT-OSS 120B does not support attachments; select a Gemini or Claude model")
				}
			}
		}
	}
	if maxTokens > 0 && call.MaxOutputTokens != nil && (*call.MaxOutputTokens <= 0 || *call.MaxOutputTokens > maxTokens) {
		return envelope{}, nil, fmt.Errorf("antigravity: %s requires max output tokens between 1 and %d", modelID, maxTokens)
	}
	system, contents, warnings := toGeminiPrompt(call.Prompt)

	req := generateContentRequest{
		Contents:          contents,
		SystemInstruction: system,
	}

	cfg := &generationConfig{}
	hasCfg := false
	samplingSupported := !strings.HasPrefix(m.modelID, "gemini-3.8-flash")
	if samplingSupported && call.Temperature != nil {
		v := float32(*call.Temperature)
		cfg.Temperature = &v
		hasCfg = true
	}
	if samplingSupported && call.TopP != nil {
		v := float32(*call.TopP)
		cfg.TopP = &v
		hasCfg = true
	}
	if samplingSupported && call.TopK != nil {
		v := float32(*call.TopK)
		cfg.TopK = &v
		hasCfg = true
	}
	if call.MaxOutputTokens != nil {
		cfg.MaxOutputTokens = int32(*call.MaxOutputTokens) //nolint: gosec
		hasCfg = true
	}

	if v, ok := call.ProviderOptions[Name]; ok {
		if opts, ok := v.(*ProviderOptions); ok && opts.ThinkingLevel != "" && !routed {
			cfg.ThinkingConfig = &thinkingConfig{
				ThinkingLevel:   opts.ThinkingLevel,
				IncludeThoughts: true,
			}
			hasCfg = true
		}
	}

	if hasCfg {
		req.GenerationConfig = cfg
	}

	if len(call.Tools) > 0 {
		var decls []functionDeclaration
		for _, tool := range call.Tools {
			ft, ok := tool.(fantasy.FunctionTool)
			if !ok {
				warnings = append(warnings, fantasy.CallWarning{
					Type:    fantasy.CallWarningTypeUnsupportedTool,
					Tool:    tool,
					Message: "tool is not supported",
				})
				continue
			}
			decls = append(decls, functionDeclaration{
				Name:        ft.Name,
				Description: ft.Description,
				Parameters:  ft.InputSchema,
			})
		}
		if len(decls) > 0 {
			req.Tools = []geminiTool{{FunctionDeclarations: decls}}
		}
	}

	return envelope{
		Project:     m.options.project,
		Model:       modelID,
		Request:     req,
		UserAgent:   "antigravity",
		RequestID:   "agent-" + uuid.NewString(),
		RequestType: "agent",
	}, warnings, nil
}

func toGeminiPrompt(prompt fantasy.Prompt) (*geminiContent, []geminiContent, []fantasy.CallWarning) {
	var system *geminiContent
	var contents []geminiContent
	var warnings []fantasy.CallWarning

	for _, msg := range prompt {
		switch msg.Role {
		case fantasy.MessageRoleSystem:
			var texts []string
			for _, part := range msg.Content {
				if t, ok := fantasy.AsMessagePart[fantasy.TextPart](part); ok && t.Text != "" {
					texts = append(texts, t.Text)
				}
			}
			if len(texts) > 0 {
				if system == nil {
					system = &geminiContent{Parts: []geminiPart{{Text: strings.Join(texts, "\n")}}}
				} else {
					system.Parts = append(system.Parts, geminiPart{Text: strings.Join(texts, "\n")})
				}
			}

		case fantasy.MessageRoleUser:
			var parts []geminiPart
			for _, part := range msg.Content {
				if file, ok := fantasy.AsMessagePart[fantasy.FilePart](part); ok {
					parts = append(parts, geminiPart{InlineData: &geminiInlineData{MIMEType: file.MediaType, Data: file.Data}})
				}
				if part.GetType() == fantasy.ContentTypeText {
					if t, ok := fantasy.AsMessagePart[fantasy.TextPart](part); ok && t.Text != "" {
						parts = append(parts, geminiPart{Text: t.Text})
					}
				}
			}
			if len(parts) > 0 {
				contents = append(contents, geminiContent{Role: "user", Parts: parts})
			}

		case fantasy.MessageRoleAssistant:
			var parts []geminiPart
			signatures := make(map[string]string)
			for _, part := range msg.Content {
				if reasoning, ok := fantasy.AsMessagePart[fantasy.ReasoningPart](part); ok {
					if metadata := google.GetReasoningMetadata(reasoning.ProviderOptions); metadata != nil {
						signatures[metadata.ToolID] = metadata.Signature
					}
				}
			}
			for _, part := range msg.Content {
				switch part.GetType() {
				case fantasy.ContentTypeReasoning:
					if reasoning, ok := fantasy.AsMessagePart[fantasy.ReasoningPart](part); ok && reasoning.Text != "" {
						metadata := google.GetReasoningMetadata(reasoning.ProviderOptions)
						if metadata != nil && metadata.ToolID == "" {
							parts = append(parts, geminiPart{Text: reasoning.Text, Thought: true, ThoughtSignature: google.PartSignature(metadata.Signature)})
							delete(signatures, "")
						}
					}
				case fantasy.ContentTypeText:
					if t, ok := fantasy.AsMessagePart[fantasy.TextPart](part); ok && t.Text != "" {
						parts = append(parts, geminiPart{Text: t.Text, ThoughtSignature: google.PartSignature(signatures[""])})
						delete(signatures, "")
					}
				case fantasy.ContentTypeToolCall:
					tc, ok := fantasy.AsMessagePart[fantasy.ToolCallPart](part)
					if !ok {
						continue
					}
					var args map[string]any
					if err := json.Unmarshal([]byte(tc.Input), &args); err != nil {
						continue
					}
					parts = append(parts, geminiPart{
						FunctionCall:     &geminiFuncCall{ID: tc.ToolCallID, Name: tc.ToolName, Args: args},
						ThoughtSignature: functionCallSignature(signatures, tc.ToolCallID),
					})
				}
			}
			if len(parts) > 0 {
				contents = append(contents, geminiContent{Role: "model", Parts: parts})
			}

		case fantasy.MessageRoleTool:
			var parts []geminiPart
			for _, part := range msg.Content {
				if part.GetType() != fantasy.ContentTypeToolResult {
					continue
				}
				result, ok := fantasy.AsMessagePart[fantasy.ToolResultPart](part)
				if !ok {
					continue
				}
				var toolName string
				for _, m := range prompt {
					if m.Role != fantasy.MessageRoleAssistant {
						continue
					}
					for _, c := range m.Content {
						if tc, ok := fantasy.AsMessagePart[fantasy.ToolCallPart](c); ok && tc.ToolCallID == result.ToolCallID {
							toolName = tc.ToolName
						}
					}
				}
				var response map[string]any
				switch result.Output.GetType() {
				case fantasy.ToolResultContentTypeText:
					if c, ok := fantasy.AsToolResultOutputType[fantasy.ToolResultOutputContentText](result.Output); ok {
						response = map[string]any{"result": c.Text}
					}
				case fantasy.ToolResultContentTypeError:
					if c, ok := fantasy.AsToolResultOutputType[fantasy.ToolResultOutputContentError](result.Output); ok {
						response = map[string]any{"result": c.Error.Error()}
					}
				}
				if response != nil {
					parts = append(parts, geminiPart{
						FunctionResponse: &geminiFuncResult{ID: result.ToolCallID, Name: toolName, Response: response},
					})
				}
			}
			if len(parts) > 0 {
				contents = append(contents, geminiContent{Role: "user", Parts: parts})
			}

		default:
			panic("unsupported message role: " + msg.Role)
		}
	}

	return system, contents, warnings
}

// -- HTTP -----------------------------------------------------------------

func (m *languageModel) do(ctx context.Context, method string, body envelope) (*http.Response, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	url := strings.TrimRight(m.options.baseURL, "/") + "/v1internal:" + method
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+m.options.apiKey)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range m.options.headers {
		req.Header.Set(k, v)
	}
	if method == "streamGenerateContent" {
		req.URL.RawQuery = "alt=sse"
		req.Header.Set("Accept", "text/event-stream")
	}

	client := m.options.client
	if client == nil {
		client = http.DefaultClient
	}
	return client.Do(req) //nolint: bodyclose // caller closes
}

// Generate implements fantasy.LanguageModel.
func (m *languageModel) Generate(ctx context.Context, call fantasy.Call) (*fantasy.Response, error) {
	env, warnings, err := m.buildEnvelope(call)
	if err != nil {
		return nil, err
	}

	// Antigravity serves generation through SSE, including non-streaming calls.
	resp, err := m.do(ctx, "streamGenerateContent", env)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}
		return nil, &fantasy.ProviderError{StatusCode: resp.StatusCode, Message: string(body)}
	}

	var gr generateContentResponse
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		data, ok := strings.CutPrefix(scanner.Text(), "data:")
		if !ok || strings.TrimSpace(data) == "" {
			continue
		}
		var chunk generateContentResponse
		if err := json.Unmarshal([]byte(strings.TrimSpace(data)), &chunk); err != nil {
			return nil, fmt.Errorf("decode antigravity response: %w", err)
		}
		if chunk.Error != nil {
			return nil, fmt.Errorf("antigravity: %s", chunk.Error.Message)
		}
		if chunk.UsageMetadata != nil {
			gr.UsageMetadata = chunk.UsageMetadata
		}
		if len(chunk.Candidates) == 0 {
			continue
		}
		if len(gr.Candidates) == 0 {
			gr.Candidates = []responseCandidate{{}}
		}
		candidate := chunk.Candidates[0]
		gr.Candidates[0].Content.Parts = append(gr.Candidates[0].Content.Parts, candidate.Content.Parts...)
		if candidate.FinishReason != "" {
			gr.Candidates[0].FinishReason = candidate.FinishReason
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read antigravity response: %w", err)
	}

	return mapResponse(gr, warnings)
}

func mapResponse(gr generateContentResponse, warnings []fantasy.CallWarning) (*fantasy.Response, error) {
	if len(gr.Candidates) == 0 {
		return nil, errors.New("antigravity: no response from model")
	}
	candidate := gr.Candidates[0]

	var content []fantasy.Content
	var hasToolCalls bool
	for _, part := range candidate.Content.Parts {
		switch {
		case part.FunctionCall != nil:
			input, err := json.Marshal(part.FunctionCall.Args)
			if err != nil {
				return nil, err
			}
			toolCallID := cmp.Or(part.FunctionCall.ID, uuid.NewString())
			if len(part.ThoughtSignature) > 0 {
				content = append(content, fantasy.ReasoningContent{ProviderMetadata: fantasy.ProviderMetadata{
					google.Name: &google.ReasoningMetadata{Signature: google.EncodeSignature(part.ThoughtSignature), ToolID: toolCallID},
				}})
			}
			content = append(content, fantasy.ToolCallContent{
				ToolCallID: toolCallID,
				ToolName:   part.FunctionCall.Name,
				Input:      string(input),
			})
			hasToolCalls = true
		case part.Thought:
			content = append(content, fantasy.ReasoningContent{Text: part.Text, ProviderMetadata: signatureMetadata(part.ThoughtSignature, "")})
		case part.Text != "":
			if len(part.ThoughtSignature) > 0 {
				content = append(content, fantasy.ReasoningContent{ProviderMetadata: signatureMetadata(part.ThoughtSignature, "")})
			}
			content = append(content, fantasy.TextContent{Text: part.Text})
		}
	}

	finishReason := mapFinishReason(candidate.FinishReason)
	if hasToolCalls {
		finishReason = fantasy.FinishReasonToolCalls
	}

	return &fantasy.Response{
		Content:      content,
		FinishReason: finishReason,
		Usage:        mapUsage(gr.UsageMetadata),
		Warnings:     warnings,
	}, nil
}

func mapUsage(u *usageMetadata) fantasy.Usage {
	if u == nil {
		return fantasy.Usage{}
	}
	// promptTokenCount includes cachedContentTokenCount; the shared Usage
	// contract keeps cache reads out of InputTokens.
	return fantasy.Usage{
		InputTokens:     max(int64(u.PromptTokenCount)-int64(u.CachedContentTokenCount), 0),
		OutputTokens:    int64(u.CandidatesTokenCount),
		TotalTokens:     int64(u.TotalTokenCount),
		ReasoningTokens: int64(u.ThoughtsTokenCount),
		CacheReadTokens: int64(u.CachedContentTokenCount),
	}
}

func mapFinishReason(reason string) fantasy.FinishReason {
	switch reason {
	case "STOP":
		return fantasy.FinishReasonStop
	case "MAX_TOKENS":
		return fantasy.FinishReasonLength
	case "SAFETY", "BLOCKLIST", "PROHIBITED_CONTENT", "SPII", "IMAGE_SAFETY":
		return fantasy.FinishReasonContentFilter
	case "RECITATION", "LANGUAGE", "MALFORMED_FUNCTION_CALL":
		return fantasy.FinishReasonError
	case "OTHER":
		return fantasy.FinishReasonOther
	case "":
		return fantasy.FinishReasonUnknown
	default:
		return fantasy.FinishReasonOther
	}
}

// Stream implements fantasy.LanguageModel. Antigravity's SSE stream carries
// the same candidates/content/parts shape as the non-streaming response,
// one full or partial GenerateContentResponse per "data:" line.
func (m *languageModel) Stream(ctx context.Context, call fantasy.Call) (fantasy.StreamResponse, error) {
	env, warnings, err := m.buildEnvelope(call)
	if err != nil {
		return nil, err
	}

	resp, err := m.do(ctx, "streamGenerateContent", env)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return nil, &fantasy.ProviderError{StatusCode: resp.StatusCode, Message: string(body)}
	}

	return func(yield func(fantasy.StreamPart) bool) {
		defer resp.Body.Close()

		if len(warnings) > 0 {
			if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeWarnings, Warnings: warnings}) {
				return
			}
		}

		var (
			blockCounter             int
			isActiveText             bool
			currentTextBlockID       string
			isActiveReasoning        bool
			currentReasoningID       string
			currentReasoningMetadata fantasy.ProviderMetadata
			usage                    fantasy.Usage
			lastFinishReason         fantasy.FinishReason
			sawToolCall              bool
		)

		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			data, ok := strings.CutPrefix(line, "data:")
			if !ok {
				continue
			}
			data = strings.TrimSpace(data)
			if data == "" {
				continue
			}

			var gr generateContentResponse
			if err := json.Unmarshal([]byte(data), &gr); err != nil {
				yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeError, Error: fmt.Errorf("decode antigravity stream chunk: %w", err)})
				return
			}
			if gr.Error != nil {
				yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeError, Error: fmt.Errorf("antigravity: %s", gr.Error.Message)})
				return
			}
			if len(gr.Candidates) == 0 {
				continue
			}
			candidate := gr.Candidates[0]

			for _, part := range candidate.Content.Parts {
				switch {
				case part.FunctionCall != nil:
					if isActiveText {
						isActiveText = false
						if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextEnd, ID: currentTextBlockID}) {
							return
						}
					}
					if isActiveReasoning {
						isActiveReasoning = false
						if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeReasoningEnd, ID: currentReasoningID, ProviderMetadata: currentReasoningMetadata}) {
							return
						}
					}
					input, err := json.Marshal(part.FunctionCall.Args)
					if err != nil {
						yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeError, Error: err})
						return
					}
					toolCallID := cmp.Or(part.FunctionCall.ID, uuid.NewString())
					if len(part.ThoughtSignature) > 0 {
						signatureID := "signature-" + toolCallID
						if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeReasoningStart, ID: signatureID}) ||
							!yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeReasoningEnd, ID: signatureID, ProviderMetadata: signatureMetadata(part.ThoughtSignature, toolCallID)}) {
							return
						}
					}
					sawToolCall = true
					if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeToolInputStart, ID: toolCallID, ToolCallName: part.FunctionCall.Name}) {
						return
					}
					if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeToolInputDelta, ID: toolCallID, Delta: string(input)}) {
						return
					}
					if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeToolInputEnd, ID: toolCallID}) {
						return
					}
					if !yield(fantasy.StreamPart{
						Type:          fantasy.StreamPartTypeToolCall,
						ID:            toolCallID,
						ToolCallName:  part.FunctionCall.Name,
						ToolCallInput: string(input),
					}) {
						return
					}

				case part.Thought:
					if isActiveText {
						isActiveText = false
						if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextEnd, ID: currentTextBlockID}) {
							return
						}
					}
					if !isActiveReasoning {
						isActiveReasoning = true
						currentReasoningMetadata = nil
						currentReasoningID = fmt.Sprintf("%d", blockCounter)
						blockCounter++
						if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeReasoningStart, ID: currentReasoningID}) {
							return
						}
					}
					if len(part.ThoughtSignature) > 0 {
						currentReasoningMetadata = signatureMetadata(part.ThoughtSignature, "")
					}
					if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeReasoningDelta, ID: currentReasoningID, Delta: part.Text}) {
						return
					}

				case part.Text != "":
					if isActiveReasoning {
						isActiveReasoning = false
						if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeReasoningEnd, ID: currentReasoningID, ProviderMetadata: currentReasoningMetadata}) {
							return
						}
					}
					if !isActiveText {
						isActiveText = true
						currentTextBlockID = fmt.Sprintf("%d", blockCounter)
						blockCounter++
						if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextStart, ID: currentTextBlockID}) {
							return
						}
					}
					if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextDelta, ID: currentTextBlockID, Delta: part.Text}) {
						return
					}
				}
			}

			if gr.UsageMetadata != nil {
				usage = mapUsage(gr.UsageMetadata)
			}
			if candidate.FinishReason != "" {
				lastFinishReason = mapFinishReason(candidate.FinishReason)
			}
		}
		if err := scanner.Err(); err != nil {
			yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeError, Error: err})
			return
		}

		if isActiveText {
			if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextEnd, ID: currentTextBlockID}) {
				return
			}
		}
		if isActiveReasoning {
			if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeReasoningEnd, ID: currentReasoningID, ProviderMetadata: currentReasoningMetadata}) {
				return
			}
		}

		finishReason := lastFinishReason
		if sawToolCall {
			finishReason = fantasy.FinishReasonToolCalls
		} else if finishReason == "" {
			yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeError, Error: fantasy.NewIncompleteStreamError()})
			return
		}

		yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeFinish, Usage: usage, FinishReason: finishReason})
	}, nil
}

func signatureMetadata(signature []byte, toolID string) fantasy.ProviderMetadata {
	if len(signature) == 0 {
		return nil
	}
	return fantasy.ProviderMetadata{google.Name: &google.ReasoningMetadata{Signature: google.EncodeSignature(signature), ToolID: toolID}}
}

// functionCallSignature returns the signature for a function call, or nil
// when the step carried none for it.
func functionCallSignature(signatures map[string]string, toolCallID string) []byte {
	signature, found := signatures[toolCallID]
	if !found || signature == "" {
		return nil
	}
	return google.FunctionCallSignature(signature)
}

// GenerateObject implements fantasy.LanguageModel via the generic
// tool-call-based fallback (no native structured-output mode here).
func (m *languageModel) GenerateObject(ctx context.Context, call fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	return object.GenerateWithTool(ctx, m, call)
}

// StreamObject implements fantasy.LanguageModel via the generic
// tool-call-based fallback.
func (m *languageModel) StreamObject(ctx context.Context, call fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	return object.StreamWithTool(ctx, m, call)
}
