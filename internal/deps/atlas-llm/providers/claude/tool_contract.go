package claude

import (
	"encoding/json"
	"math"

	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm/schema"
)

type claudeToolBinding struct {
	name   string
	schema schema.Schema
}

func claudeToolBindings(tools []fantasy.Tool) map[string]claudeToolBinding {
	bindings := map[string]claudeToolBinding{}
	aliases := map[string]claudeToolBinding{}
	for _, tool := range tools {
		if t, ok := tool.(fantasy.FunctionTool); ok {
			data, err := json.Marshal(t.InputSchema)
			if err != nil {
				continue
			}
			var s schema.Schema
			if json.Unmarshal(data, &s) != nil {
				continue
			}
			bindings[claudeToolPrefix+t.Name] = claudeToolBinding{name: t.Name, schema: s}
			// Some responses use the original name after seeing local history.
			aliases[t.Name] = claudeToolBinding{name: t.Name, schema: s}
		}
	}
	for name, alias := range aliases {
		if _, exists := bindings[name]; !exists {
			bindings[name] = alias
		}
	}
	return bindings
}

// Claude history uses wire names without mutating the stored local conversation.
func claudeToolHistory(prompt fantasy.Prompt, tools []fantasy.Tool) fantasy.Prompt {
	names := map[string]string{}
	for _, tool := range tools {
		if t, ok := tool.(fantasy.FunctionTool); ok {
			names[t.Name] = claudeToolPrefix + t.Name
		}
	}
	copyPrompt := append(fantasy.Prompt(nil), prompt...)
	for i, message := range copyPrompt {
		message.Content = append([]fantasy.MessagePart(nil), message.Content...)
		for j, content := range message.Content {
			switch c := content.(type) {
			case fantasy.ToolCallPart:
				if name, exists := names[c.ToolName]; exists && !c.ProviderExecuted {
					c.ToolName = name
					message.Content[j] = c
				}
			}
		}
		copyPrompt[i] = message
	}
	return copyPrompt
}

// Decode JSON encoded fields only where the advertised schema expects that type.
// Text, unknown fields and invalid candidates remain untouched. Validation must
// succeed for the whole candidate before normal tool hooks see the decoded call.
func normalizeClaudeArguments(input string, spec schema.Schema) string {
	if len(input) > 64*1024 {
		return input
	}
	var value any
	if json.Unmarshal([]byte(input), &value) != nil {
		return input
	}
	changed := false
	value = decodeClaudeValue(value, spec, 0, &changed)
	if !changed || schema.ValidateAgainstSchema(value, spec) != nil {
		return input
	}
	data, err := json.Marshal(value)
	if err != nil {
		return input
	}
	return string(data)
}

func decodeClaudeValue(value any, spec schema.Schema, depth int, changed *bool) any {
	if depth > 16 {
		return value
	}
	if text, ok := value.(string); ok && (spec.Type == "object" || spec.Type == "array" || spec.Type == "integer") {
		var decoded any
		if json.Unmarshal([]byte(text), &decoded) == nil {
			valid := false
			switch spec.Type {
			case "object":
				_, valid = decoded.(map[string]any)
			case "array":
				_, valid = decoded.([]any)
			case "integer":
				if n, number := decoded.(float64); number {
					valid = math.Trunc(n) == n && math.Abs(n) < 1<<53
				}
			}
			if valid {
				value, *changed = decoded, true
			}
		}
	}
	switch object := value.(type) {
	case map[string]any:
		for key, v := range object {
			if property, exists := spec.Properties[key]; exists && property != nil {
				object[key] = decodeClaudeValue(v, *property, depth+1, changed)
			}
		}
	case []any:
		if spec.Items != nil {
			for i, v := range object {
				object[i] = decodeClaudeValue(v, *spec.Items, depth+1, changed)
			}
		}
	}
	return value
}
