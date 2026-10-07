package anthropic

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBoundedToolSchemaPreservesShallowSchemas(t *testing.T) {
	t.Parallel()
	schema := map[string]any{"type": "object", "properties": map[string]any{"value": map[string]any{"type": "string", "enum": []any{"a", "b"}}}, "required": []any{"value"}, "additionalProperties": false}
	require.Equal(t, schema, boundedToolSchema(schema, 1, 10))
}

func TestBoundedToolSchemaHandlesArraysAndUnions(t *testing.T) {
	t.Parallel()
	for _, keyword := range []string{"items", "oneOf", "$defs", "default"} {
		t.Run(keyword, func(t *testing.T) {
			schema := map[string]any{"type": "string", "enum": []any{"retained-constraint"}}
			for range 12 {
				switch keyword {
				case "oneOf":
					schema = map[string]any{keyword: []any{schema}}
				case "$defs":
					schema = map[string]any{keyword: map[string]any{"nested": schema}}
				default:
					schema = map[string]any{keyword: schema}
				}
			}
			original, err := json.Marshal(schema)
			require.NoError(t, err)
			bounded := boundedToolSchema(schema, 1, 10)
			require.LessOrEqual(t, schemaContainerDepth(bounded), 10)
			serialized, err := json.Marshal(bounded)
			require.NoError(t, err)
			require.Contains(t, string(serialized), "retained-constraint")
			after, err := json.Marshal(schema)
			require.NoError(t, err)
			require.Equal(t, original, after)
		})
	}
}
