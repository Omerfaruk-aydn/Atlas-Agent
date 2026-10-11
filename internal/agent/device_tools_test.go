package agent

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

const deviceToolsConfig = `, "tools": {"devices": {"enabled": true}, "browser": {"enabled": true}}`

var deviceToolNames = []string{"android", "app_loop", "isolated_desktop", "remote_node", "screen_mark"}

// A greeting must not carry any device schema; the capability index names
// them so they can be loaded on demand.
func TestDeviceToolsStayDeferredOnGreeting(t *testing.T) {
	t.Setenv("ATLAS_DEVICES_DIR", t.TempDir())
	h := newPayloadCoordinator(t, wireOpenAI, "", deviceToolsConfig)
	_, err := h.coord.Run(t.Context(), newSession(t, h), "merhaba")
	require.NoError(t, err)
	steps := stepTools(t, h)
	for _, name := range deviceToolNames {
		require.NotContains(t, steps[0], name)
	}
	require.NotContains(t, steps[0], "remote_support", "remote support loads only when a task needs it")
	body := stepBody(t, h, 0)
	require.Contains(t, body, "devices: android, app_loop, isolated_desktop, remote_node, remote_support, screen_mark")
}

// Discovery loads every device schema for the next step, each provider
// family serializes it, and a real call round-trips through the provider
// with a machine-readable result. Unknown arguments are rejected without
// running anything.
func TestDeviceToolsSerializeAndRunAcrossProviders(t *testing.T) {
	for _, format := range []wireFormat{wireOpenAI, wireAnthropic, wireGemini} {
		t.Run(string(format), func(t *testing.T) {
			t.Setenv("ATLAS_DEVICES_DIR", t.TempDir())
			h := newPayloadCoordinator(t, format, "", deviceToolsConfig,
				toolCallReply(format, "c1", "tool_search", map[string]any{"names": deviceToolNames}),
				toolCallReply(format, "c2", "remote_node", map[string]any{"action": "list"}),
				toolCallReply(format, "c3", "remote_node", map[string]any{"action": "list", "bogus": true}),
				textReply(format, "done"),
			)
			_, err := h.coord.Run(t.Context(), newSession(t, h), "use my paired computers")
			require.NoError(t, err)
			steps := stepTools(t, h)
			require.Len(t, steps, 4)
			for _, name := range deviceToolNames {
				require.NotContains(t, steps[0], name)
				require.Contains(t, steps[1], name, "discovered schemas are advertised on the next step")
			}
			// Every device schema reached the wire with its action enum.
			body := stepBody(t, h, 1)
			for _, action := range []string{"capabilities", "verify_text", "observation_id", "probe_kind", "request"} {
				require.Contains(t, body, action)
			}
			listed := stepBody(t, h, 2)
			require.Contains(t, listed, `node \\\"local\\\"`, "the local pseudo-node is always listed")
			require.Contains(t, listed, "atlas node serve", "an empty registry explains how to pair instead of installing anything")
			rejected := stepBody(t, h, 3)
			require.Contains(t, rejected, "unknown field")
			require.Contains(t, rejected, "bogus")
		})
	}
}

func TestDeviceSchemasAreShallow(t *testing.T) {
	t.Setenv("ATLAS_DEVICES_DIR", t.TempDir())
	h := newPayloadCoordinator(t, wireOpenAI, "", deviceToolsConfig,
		toolCallReply(wireOpenAI, "c1", "tool_search", map[string]any{"names": deviceToolNames}),
		textReply(wireOpenAI, "done"),
	)
	_, err := h.coord.Run(t.Context(), newSession(t, h), "load device tools")
	require.NoError(t, err)
	var body struct {
		Tools []struct {
			Function struct {
				Name       string         `json:"name"`
				Parameters map[string]any `json:"parameters"`
			} `json:"function"`
		} `json:"tools"`
	}
	requests := h.recorder.stepRequests()
	data, _ := json.Marshal(requests[1])
	require.NoError(t, json.Unmarshal(data, &body))
	seen := 0
	for _, tool := range body.Tools {
		if !slices.Contains(deviceToolNames, tool.Function.Name) {
			continue
		}
		seen++
		depth := objectDepth(tool.Function.Parameters)
		limit := 3 // object → array of flat step objects → scalar fields
		if tool.Function.Name == "app_loop" {
			limit = 4 // the scenario reuses the scenario tool's contract
		}
		require.LessOrEqual(t, depth, limit, "%s schema nests %d object levels", tool.Function.Name, depth)
		props, _ := tool.Function.Parameters["properties"].(map[string]any)
		action, _ := props["action"].(map[string]any)
		require.NotEmpty(t, action["enum"], "%s action must be an enum", tool.Function.Name)
	}
	require.Equal(t, len(deviceToolNames), seen)
}

func objectDepth(node any) int {
	m, ok := node.(map[string]any)
	if !ok {
		return 0
	}
	best := 0
	if props, ok := m["properties"].(map[string]any); ok {
		for _, p := range props {
			best = max(best, objectDepth(p))
		}
		return best + 1
	}
	if items, ok := m["items"]; ok {
		return objectDepth(items)
	}
	return 0
}

func TestDeviceIntentsNeedTheDeviceNamed(t *testing.T) {
	t.Setenv("ATLAS_DEVICES_DIR", t.TempDir())
	for _, tc := range []struct {
		prompt string
		want   []string
		reject []string
	}{
		{"Android emülatörde giriş formunu doldur", []string{"android"}, []string{"remote_node", "screen_mark"}},
		{"Windows Sandbox içinde not defterini aç", []string{"isolated_desktop"}, []string{"android"}},
		{"README.md markdown dosyasını düzenle", nil, deviceToolNames},
		{"node.js ile küçük bir api yaz", nil, deviceToolNames},
		{"testleri wsl tarafında çalıştır, sonra burada doğrula", []string{"remote_node"}, []string{"android"}},
	} {
		h := newPayloadCoordinator(t, wireOpenAI, "", deviceToolsConfig)
		_, err := h.coord.Run(t.Context(), newSession(t, h), tc.prompt)
		require.NoError(t, err)
		first := stepTools(t, h)[0]
		for _, name := range tc.want {
			require.Contains(t, first, name, tc.prompt)
		}
		for _, name := range tc.reject {
			require.NotContains(t, first, name, tc.prompt)
		}
	}
}
