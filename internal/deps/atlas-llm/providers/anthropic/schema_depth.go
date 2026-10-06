package anthropic

import "encoding/json"

// Bound the serialized tool rather than SDK extra fields, which do not
// replace already populated schema properties.
func boundToolJSON(raw []byte, limit int) ([]byte, error) {
	var tool map[string]any
	if err := json.Unmarshal(raw, &tool); err != nil {
		return nil, err
	}
	if schema, ok := tool["input_schema"].(map[string]any); ok {
		tool["input_schema"] = boundedToolSchema(schema, 1, limit)
	}
	return json.Marshal(tool)
}

// WithToolSchemaMaxDepth bounds tool schemas for compatible APIs with lower
// limits. Zero preserves Anthropic's ordinary schema serialization.
func WithToolSchemaMaxDepth(depth int) Option {
	return func(o *options) { o.toolSchemaMaxDepth = depth }
}

// boundedToolSchema changes only the wire schema. Deep constraints remain in
// textual guidance; the agent's original schema still validates every input.
func boundedToolSchema(schema map[string]any, depth, limit int) map[string]any {
	if limit < 3 || schemaContainerDepth(schema)+depth-1 <= limit {
		return schema
	}
	if depth >= limit-2 {
		data, _ := json.Marshal(schema)
		return map[string]any{"description": "Use a JSON value matching this schema (validated locally): " + string(data)}
	}
	out := make(map[string]any, len(schema))
	for key, value := range schema {
		switch key {
		case "properties", "$defs", "definitions", "patternProperties":
			if fields, ok := value.(map[string]any); ok {
				copyFields := make(map[string]any, len(fields))
				for name, field := range fields {
					if nested, ok := field.(map[string]any); ok {
						copyFields[name] = boundedToolSchema(nested, depth+2, limit)
					} else {
						copyFields[name] = field
					}
				}
				out[key] = copyFields
				continue
			}
		case "items", "additionalProperties", "not", "if", "then", "else", "contains":
			if nested, ok := value.(map[string]any); ok {
				out[key] = boundedToolSchema(nested, depth+1, limit)
				continue
			}
		case "anyOf", "oneOf", "allOf", "prefixItems":
			if branches, ok := value.([]any); ok {
				copyBranches := make([]any, len(branches))
				for i, branch := range branches {
					if nested, ok := branch.(map[string]any); ok {
						copyBranches[i] = boundedToolSchema(nested, depth+2, limit)
					} else {
						copyBranches[i] = branch
					}
				}
				out[key] = copyBranches
				continue
			}
		}
		out[key] = value
	}
	// Unusual nested enum/default/extension values also count toward the API
	// limit. Preserve their full meaning in guidance rather than invalid JSON.
	if schemaContainerDepth(out)+depth-1 > limit {
		data, _ := json.Marshal(schema)
		return map[string]any{"description": "Use a JSON value matching this schema (validated locally): " + string(data)}
	}
	return out
}

func schemaContainerDepth(value any) int {
	depth := 0
	switch value := value.(type) {
	case map[string]any:
		for _, child := range value {
			depth = max(depth, schemaContainerDepth(child))
		}
		return depth + 1
	case []any:
		for _, child := range value {
			depth = max(depth, schemaContainerDepth(child))
		}
		return depth + 1
	case []string:
		return 1
	}
	return 0
}
