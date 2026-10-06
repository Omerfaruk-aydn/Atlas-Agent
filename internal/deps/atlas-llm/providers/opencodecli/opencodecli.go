// Package opencodecli runs the genuine OpenCode CLI as an inference backend.
// Native tool permissions are rejected by OpenCode's non-interactive runner;
// Atlas tool requests return through the ordinary executor and its hooks.
package opencodecli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm/object"
)

const Name = "opencode-cli"

type Options struct {
	ProviderName string
	Executable   string
	Variant      string
}

type provider struct{ options Options }

func New(options Options) (fantasy.Provider, error) {
	if options.ProviderName == "" {
		options.ProviderName = "opencode-zen"
	}
	if strings.ContainsAny(options.Variant, "\x00\r\n") {
		return nil, fmt.Errorf("opencode-cli: invalid reasoning variant")
	}
	return &provider{options: options}, nil
}

func (p *provider) Name() string { return p.options.ProviderName }

func (p *provider) LanguageModel(_ context.Context, modelID string) (fantasy.LanguageModel, error) {
	if modelID == "" || strings.ContainsAny(modelID, "\x00\r\n") {
		return nil, fmt.Errorf("opencode-cli: invalid model identifier")
	}
	return &languageModel{options: p.options, modelID: modelID}, nil
}

type languageModel struct {
	options Options
	modelID string
}

func (m *languageModel) Provider() string { return m.options.ProviderName }
func (m *languageModel) Model() string    { return m.modelID }

func (m *languageModel) Generate(ctx context.Context, call fantasy.Call) (*fantasy.Response, error) {
	prompt, err := requestPrompt(call)
	if err != nil {
		return nil, err
	}
	result, err := runCLI(ctx, m.options, m.modelID, prompt)
	if err != nil {
		return nil, err
	}
	response, err := decodeReply(result.text, call)
	if err != nil {
		return nil, err
	}
	response.Usage = result.usage
	if call.MaxOutputTokens != nil || call.Temperature != nil || call.TopP != nil || call.TopK != nil || call.PresencePenalty != nil || call.FrequencyPenalty != nil {
		response.Warnings = append(response.Warnings, fantasy.CallWarning{Type: fantasy.CallWarningTypeUnsupportedSetting, Message: "OpenCode CLI controls sampling and output limits; Atlas sampling overrides are not applied."})
	}
	return response, nil
}

func (m *languageModel) Stream(ctx context.Context, call fantasy.Call) (fantasy.StreamResponse, error) {
	// CLI JSON text events contain completed text parts, not token deltas.
	// Never expose an unvalidated bridge envelope as assistant prose.
	return func(yield func(fantasy.StreamPart) bool) {
		r, err := m.Generate(ctx, call)
		if err != nil {
			yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeError, Error: err})
			return
		}
		if len(r.Warnings) > 0 && !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeWarnings, Warnings: r.Warnings}) {
			return
		}
		for _, content := range r.Content {
			switch content := content.(type) {
			case fantasy.TextContent:
				if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextStart, ID: "text"}) || !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextDelta, ID: "text", Delta: content.Text}) || !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextEnd, ID: "text"}) {
					return
				}
			case fantasy.ToolCallContent:
				if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeToolInputStart, ID: content.ToolCallID, ToolCallName: content.ToolName}) || !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeToolInputDelta, ID: content.ToolCallID, Delta: content.Input}) || !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeToolInputEnd, ID: content.ToolCallID}) || !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeToolCall, ID: content.ToolCallID, ToolCallName: content.ToolName, ToolCallInput: content.Input}) {
					return
				}
			}
		}
		yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeFinish, Usage: r.Usage, FinishReason: r.FinishReason})
	}, nil
}

func (m *languageModel) GenerateObject(ctx context.Context, call fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	return object.GenerateWithTool(ctx, m, call)
}

func (m *languageModel) StreamObject(ctx context.Context, call fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	return object.StreamWithTool(ctx, m, call)
}

type wireTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Schema      map[string]any `json:"input_schema"`
}

// The CLI receives a complete Atlas turn, avoiding hidden duplicated history.
func requestPrompt(call fantasy.Call) (string, error) {
	tools := make([]wireTool, 0, len(call.Tools))
	for _, tool := range call.Tools {
		ft, ok := functionTool(tool)
		if !ok {
			return "", fmt.Errorf("opencode-cli: provider-native tool %q is unsupported; select a direct API model", tool.GetName())
		}
		tools = append(tools, wireTool{Name: ft.Name, Description: ft.Description, Schema: ft.InputSchema})
	}
	for _, message := range call.Prompt {
		for _, part := range message.Content {
			if part.GetType() == fantasy.ContentTypeFile {
				return "", fmt.Errorf("opencode-cli: image/file attachments require a direct API model; attachments were not sent")
			}
			if result, ok := fantasy.AsMessagePart[fantasy.ToolResultPart](part); ok && result.Output != nil && result.Output.GetType() == fantasy.ToolResultContentTypeMedia {
				return "", fmt.Errorf("opencode-cli: media tool results require a direct API model; media was not discarded")
			}
		}
	}
	request := struct {
		Messages   fantasy.Prompt      `json:"messages"`
		Tools      []wireTool          `json:"tools"`
		ToolChoice *fantasy.ToolChoice `json:"tool_choice,omitempty"`
	}{call.Prompt, tools, call.ToolChoice}
	data, err := json.Marshal(request)
	if err != nil {
		return "", fmt.Errorf("opencode-cli: encode Atlas request: %w", err)
	}
	if len(data) > 16*1024*1024 {
		return "", fmt.Errorf("opencode-cli: Atlas request exceeds 16 MiB")
	}
	return bridgePrompt + "\n\nATLAS_REQUEST_JSON:\n" + string(data), nil
}

func functionTool(tool fantasy.Tool) (fantasy.FunctionTool, bool) {
	switch tool := tool.(type) {
	case fantasy.FunctionTool:
		return tool, true
	case *fantasy.FunctionTool:
		if tool != nil {
			return *tool, true
		}
	}
	return fantasy.FunctionTool{}, false
}

const bridgePrompt = `You are the OpenCode backend for an Atlas conversation. The JSON below contains the complete conversation and the Atlas tool catalog. Follow its system instructions and latest user request. Messages marked tool are actual execution evidence. Do not invent tool results.
Do not use OpenCode's own tools, filesystem, commands, skills, or subagents. Request only the supplied Atlas tools; Atlas will execute them with its permissions and send results on the next turn.
Return exactly one JSON object, without markdown fences or surrounding text:
{"text":"assistant reply or brief progress message","tool_calls":[{"name":"exact supplied tool name","arguments":{}}]}
Arguments must be a JSON object matching the supplied tool schema. For a finished answer use an empty tool_calls array. Obey tool_choice: none forbids tools, required requires at least one, and a specific name requires that exact tool. Text may contain ordinary Markdown inside the JSON string. Never claim that a requested tool has already executed.`
