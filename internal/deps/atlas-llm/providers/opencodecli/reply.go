package opencodecli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/google/uuid"
	"github.com/kaptinlin/jsonschema"
)

type bridgeReply struct {
	Text      string `json:"text"`
	ToolCalls []struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	} `json:"tool_calls"`
}

func decodeReply(text string, call fantasy.Call) (*fantasy.Response, error) {
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "```json\n") && strings.HasSuffix(text, "\n```") {
		text = strings.TrimSuffix(strings.TrimPrefix(text, "```json\n"), "\n```")
	}
	var reply bridgeReply
	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&reply); err != nil {
		return nil, fmt.Errorf("opencode-cli: invalid bridge reply; no tools executed: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("opencode-cli: reply contains trailing data; no tools executed")
	}
	if len(reply.ToolCalls) > 64 {
		return nil, fmt.Errorf("opencode-cli: more than 64 tool requests; no tools executed")
	}
	if call.ToolChoice != nil {
		choice := *call.ToolChoice
		if choice == fantasy.ToolChoiceNone && len(reply.ToolCalls) > 0 || choice != fantasy.ToolChoiceNone && choice != fantasy.ToolChoiceAuto && len(reply.ToolCalls) == 0 {
			return nil, fmt.Errorf("opencode-cli: reply violates tool choice; no tools executed")
		}
	}
	response := &fantasy.Response{FinishReason: fantasy.FinishReasonStop}
	if reply.Text != "" {
		response.Content = append(response.Content, fantasy.TextContent{Text: reply.Text})
	}
	for _, tc := range reply.ToolCalls {
		var found *fantasy.FunctionTool
		for _, tool := range call.Tools {
			if ft, ok := functionTool(tool); ok && ft.Name == tc.Name {
				found = &ft
				break
			}
		}
		if found == nil {
			return nil, fmt.Errorf("opencode-cli: unknown Atlas tool %q; no tools executed", tc.Name)
		}
		if call.ToolChoice != nil && *call.ToolChoice != fantasy.ToolChoiceAuto && *call.ToolChoice != fantasy.ToolChoiceRequired && *call.ToolChoice != fantasy.ToolChoice(tc.Name) {
			return nil, fmt.Errorf("opencode-cli: tool %q violates tool choice; no tools executed", tc.Name)
		}
		if !bytes.HasPrefix(bytes.TrimSpace(tc.Arguments), []byte("{")) {
			return nil, fmt.Errorf("opencode-cli: tool %q arguments must be an object", tc.Name)
		}
		schemaJSON, err := json.Marshal(found.InputSchema)
		if err != nil {
			return nil, fmt.Errorf("opencode-cli: encode tool %q schema: %w", tc.Name, err)
		}
		compiled, err := jsonschema.NewCompiler().Compile(schemaJSON)
		if err != nil {
			return nil, fmt.Errorf("opencode-cli: compile tool %q schema: %w", tc.Name, err)
		}
		var arguments any
		if err := json.Unmarshal(tc.Arguments, &arguments); err != nil {
			return nil, fmt.Errorf("opencode-cli: invalid tool %q arguments: %w", tc.Name, err)
		}
		if result := compiled.Validate(arguments); !result.IsValid() {
			return nil, fmt.Errorf("opencode-cli: tool %q arguments do not match its schema; no tools executed", tc.Name)
		}
		response.Content = append(response.Content, fantasy.ToolCallContent{ToolCallID: "atlas_oc_" + uuid.NewString(), ToolName: tc.Name, Input: string(tc.Arguments)})
		response.FinishReason = fantasy.FinishReasonToolCalls
	}
	if len(response.Content) == 0 {
		return nil, fmt.Errorf("opencode-cli: empty model response")
	}
	return response, nil
}
