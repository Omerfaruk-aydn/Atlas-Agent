package agent

import (
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/interaction"
	"github.com/stretchr/testify/require"
)

func TestCancelDuringHandoffIsPersistedForThatConversationOnly(t *testing.T) {
	root := t.TempDir()
	const paused, other = "cancel-handoff-fixture", "cancel-other-fixture"
	t.Cleanup(func() { interaction.Default.Resume(paused); interaction.Default.Resume(other) })
	c := &coordinator{cfg: config.NewTestStore(&config.Config{Options: &config.Options{}}).Scoped(root)}

	interaction.Default.Pause(paused, "Approve the sign-in on your phone")
	require.NoError(t, interaction.Default.Record(root, paused, interaction.Entry{Resource: "browser", Action: "handoff", Status: "paused"}))
	c.recordInteractionCancel(paused)
	c.recordInteractionCancel(other)

	require.True(t, interaction.Default.Snapshot(paused).Canceled)
	require.False(t, interaction.Default.Snapshot(other).Canceled, "a conversation without a handoff has nothing to cancel")
	restored := interaction.New()
	require.NoError(t, restored.Load(root, paused))
	require.True(t, restored.Snapshot(paused).Canceled, "the cancellation must survive a restart")
}
