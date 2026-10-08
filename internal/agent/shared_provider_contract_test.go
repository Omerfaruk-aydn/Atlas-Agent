package agent

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Recorded wire fixtures verify transport contracts, not live model quality.
func TestSharedVerificationContractAcrossProviderFormats(t *testing.T) {
	for _, format := range []wireFormat{wireOpenAI, wireAnthropic, wireGemini} {
		t.Run(string(format), func(t *testing.T) {
			callID := "call_1"
			if format == wireAnthropic {
				callID = "toolu_1"
			}
			h := newPayloadCoordinator(t, format, "", automationTools,
				toolCallReply(format, callID, "verify", map[string]any{"action": "run", "task_id": "acceptance", "checks": []map[string]any{{"name": "refuse mutation as check", "tool": "computer", "input": map[string]any{"action": "type", "text": "must not be sent"}}}}),
				toolCallReply(format, callID+"_loaded", "verify", map[string]any{"action": "run", "task_id": "acceptance", "checks": []map[string]any{{"name": "refuse mutation as check", "tool": "computer", "input": map[string]any{"action": "type", "text": "must not be sent"}}}}),
				textReply(format, "The invalid check was refused."),
			)
			_, err := h.coord.Run(t.Context(), newSession(t, h), "Doğrulama aracıyla bilgisayar görevinin kabul kontrolünü çalıştır")
			require.NoError(t, err)
			schema := requestToolSchema(t, format, stepBody(t, h, 1), "verify")
			require.Contains(t, schema["properties"], "task_id")
			require.Contains(t, stepBody(t, h, 2), "require read-only action assert")
		})
	}
}
