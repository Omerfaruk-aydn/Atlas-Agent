package tools

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm/schema"
	"github.com/stretchr/testify/require"
)

// collectPaths lists every property path with its type, enum and required
// list, ignoring descriptions.
func collectPaths(prefix string, node map[string]any, out map[string]string) {
	signature, _ := json.Marshal(map[string]any{"type": node["type"], "enum": node["enum"], "required": node["required"]})
	out[prefix] = string(signature)
	if props, ok := node["properties"].(map[string]any); ok {
		for name, child := range props {
			collectPaths(prefix+"."+name, child.(map[string]any), out)
		}
	}
	if items, ok := node["items"].(map[string]any); ok {
		collectPaths(prefix+"[]", items, out)
	}
}

func TestPipelineSchemaKeepsRuntimeContractWhileShrinking(t *testing.T) {
	t.Parallel()
	info := NewToolPipeline(nil).Info()
	compact := map[string]string{}
	for name, value := range info.Parameters {
		collectPaths(name, value.(map[string]any), compact)
	}
	raw := schema.Generate(reflect.TypeFor[PipelineParams]())
	full := map[string]string{}
	for name, value := range schema.ToParameters(raw) {
		collectPaths(name, value.(map[string]any), full)
	}
	for path, signature := range full {
		if containsSegment(path, "batch") && containsSegment(path, "desktop") {
			require.NotContains(t, compact, path, "nested desktop batches are rejected at runtime")
			continue
		}
		if compactSignature, ok := compact[path]; ok {
			if strings.HasSuffix(path, ".action") && containsSegment(path, "desktop") {
				continue // batch removed from the enum below
			}
			require.Equal(t, signature, compactSignature, path)
		} else {
			t.Fatalf("schema path %s disappeared", path)
		}
	}
	action := info.Parameters["desktop"].(map[string]any)["properties"].(map[string]any)["input"].(map[string]any)["properties"].(map[string]any)["action"].(map[string]any)
	require.NotContains(t, action["enum"], "batch")
	require.Contains(t, action["enum"], "click")

	// The desktop recipes and browser workflow stay available.
	desktop := info.Parameters["desktop"].(map[string]any)["properties"].(map[string]any)
	for _, field := range []string{"flow", "adaptive_steps", "steps", "transition", "rename", "input", "inputs", "wait_for"} {
		require.Contains(t, desktop, field)
	}
	require.Contains(t, info.Parameters, "browser")
	require.Contains(t, info.Parameters, "steps")

	before, _ := json.Marshal(schema.ToParameters(raw))
	after, _ := json.Marshal(info.Parameters)
	require.Less(t, len(after)*2, len(before), "before=%d after=%d", len(before), len(after))
}

func containsSegment(path, segment string) bool {
	for _, part := range splitPath(path) {
		if part == segment {
			return true
		}
	}
	return false
}

func splitPath(path string) []string {
	var parts []string
	start := 0
	for i := 0; i <= len(path); i++ {
		if i == len(path) || path[i] == '.' || path[i] == '[' {
			if i > start {
				parts = append(parts, path[start:i])
			}
			start = i + 1
			if i < len(path) && path[i] == '[' {
				start = i + 2
				i++
			}
		}
	}
	return parts
}
