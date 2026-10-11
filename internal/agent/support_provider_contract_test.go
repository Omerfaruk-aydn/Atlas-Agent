package agent

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// These are recorded wire fixtures, not live model behavior measurements.
func TestRemoteSupportNativeArgumentsAcrossProviderFamilies(t *testing.T) {
	for _, format := range []wireFormat{wireOpenAI, wireAnthropic, wireGemini} {
		t.Run(string(format), func(t *testing.T) {
			t.Setenv("ATLAS_DEVICES_DIR", t.TempDir())
			h := newPayloadCoordinator(t, format, "", deviceToolsConfig,
				toolCallReply(format, "load-support", "tool_search", map[string]any{"names": []string{"remote_support"}}),
				toolCallReply(format, "remote-native", "remote_support", map[string]any{
					"action": "tool", "device_id": "fixture-device", "session_id": "fixture-session", "generation": 1,
					"tool": "browser", "arguments": map[string]any{"action": "set_field", "advanced": map[string]any{"label": "Name", "value": "fixture value"}},
				}),
				textReply(format, "No remote computer is connected."),
			)
			_, err := h.coord.Run(t.Context(), newSession(t, h), "Use remote_support on the connected computer")
			require.NoError(t, err)
			body := stepBody(t, h, 2)
			require.Contains(t, body, "no_support_target", "nested arguments must reach the tool, not fail the provider schema")
			require.Contains(t, body, "none", "there must be no local fallback input")
		})
	}
}
