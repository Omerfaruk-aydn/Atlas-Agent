package anthropic

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBoundedToolSchemaPreservesShallowSchemas(t *testing.T) {
	t.Parallel()
	schema := map[string]any{"type": "object", "properties": map[string]any{"value": map[string]any{"type": "string", "enum": []any{"a", "b"}}}, "required": []any{"value"}, "additionalProperties": false}
	require.Equal(t, schema, boundedToolSchema(schema, 1, 10))
}
