package tools

import (
	"encoding/json"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm/schema"
	"github.com/stretchr/testify/require"
)

// browserCallSchema is the schema the nested dispatcher checks a child
// browser call against (see coordinator dispatchComputer).
func browserCallSchema(t *testing.T) schema.Schema {
	t.Helper()
	info := NewBrowserTool(nil, t.TempDir(), config.ToolBrowser{}).Info()
	encoded, err := json.Marshal(map[string]any{"type": "object", "properties": info.Parameters, "required": info.Required})
	require.NoError(t, err)
	var spec schema.Schema
	require.NoError(t, json.Unmarshal(encoded, &spec))
	return spec
}

// TestBrowserWorkflowStepsMatchBrowserSchema replays the two workflow calls
// that failed in session aaae8f0a with "Property {property} does not match
// the schema". The compiler encoded an unset newer_than as year 1 and the
// schema described time as an object, so every compiled step was rejected
// before any input was sent.
func TestBrowserWorkflowStepsMatchBrowserSchema(t *testing.T) {
	t.Parallel()
	spec := browserCallSchema(t)
	for _, raw := range []string{
		`{"steps":[{"id":"name","action":"set_field","ref":"e173","text":"Atlas — Remote Mobile","target":{"commit":"enter"}},{"id":"shot","action":"screenshot"}]}`,
		`{"steps":[{"id":"frame","action":"key","key":"f"},{"id":"draw","action":"pointer_drag","target":{"viewport_id":"vp-1","x":510,"y":140,"to_x":835,"to_y":640}},{"id":"observe","action":"observe"}]}`,
		`{"steps":[{"id":"c","action":"click","ref":"e1","verify":{"condition":"text","selector":"body","expected":"ok"}}]}`,
	} {
		var p BrowserWorkflowParams
		require.NoError(t, json.Unmarshal([]byte(raw), &p))
		steps, err := compileBrowserWorkflow(p)
		require.NoError(t, err)
		for _, step := range steps {
			encoded, err := json.Marshal(step.Arguments)
			require.NoError(t, err)
			require.NotContains(t, string(encoded), "0001-01-01", "unset newer_than must not be sent")
			var value any
			require.NoError(t, json.Unmarshal(encoded, &value))
			require.NoError(t, schema.ValidateAgainstSchema(value, spec), "step %s: %s", step.ID, encoded)
		}
	}
}
