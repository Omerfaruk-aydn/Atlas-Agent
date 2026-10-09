package agent

import (
	"strings"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
	"github.com/stretchr/testify/require"
)

func historyMentions(history []fantasy.Message, text string) bool {
	for _, msg := range history {
		for _, part := range msg.Content {
			if p, ok := fantasy.AsMessagePart[fantasy.TextPart](part); ok && strings.Contains(p.Text, text) {
				return true
			}
		}
	}
	return false
}

func TestYoloReminderFollowsLiveModeForMainAndSubAgents(t *testing.T) {
	t.Parallel()
	for _, sub := range []bool{false, true} {
		perms := permission.NewPermissionService(t.TempDir(), true, nil)
		a := &sessionAgent{permissions: perms, isSubAgent: sub}
		history, _ := a.preparePrompt(nil, true)
		require.True(t, historyMentions(history, "YOLO mode is on"), "sub-agent=%v", sub)

		perms.SetSkipRequests(false)
		history, _ = a.preparePrompt(nil, true)
		require.False(t, historyMentions(history, "YOLO mode is on"), "manual mode must not claim YOLO, sub-agent=%v", sub)
	}
}
