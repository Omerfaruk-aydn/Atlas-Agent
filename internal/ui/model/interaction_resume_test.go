package model

import (
	"context"
	"errors"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/interaction"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/util"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/workspace"
	"github.com/stretchr/testify/require"
)

type interactionResumeWorkspace struct {
	workspace.Workspace
	snapshot                engineering.WorkflowSnapshot
	snapshotErr, controlErr error
	controls                []engineering.WorkflowControl
	ids                     []string
}

func (w *interactionResumeWorkspace) WorkflowSnapshot(ctx context.Context, id string) (engineering.WorkflowSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return engineering.WorkflowSnapshot{}, err
	}
	w.ids = append(w.ids, id)
	return w.snapshot, w.snapshotErr
}

func (w *interactionResumeWorkspace) WorkflowControl(ctx context.Context, id string, action engineering.WorkflowControl) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	w.ids = append(w.ids, id)
	w.controls = append(w.controls, action)
	return w.controlErr
}

func TestResumeUseRequiresPausedStateAndCurrentRevision(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name                string
		paused              bool
		readErr, controlErr error
	}{
		{name: "paused", paused: true},
		{name: "already active"},
		{name: "load failed", paused: true, readErr: errors.New("load failed")},
		{name: "revision conflict", paused: true, controlErr: errors.New("revision conflict")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ws := &interactionResumeWorkspace{snapshot: engineering.WorkflowSnapshot{Revision: "current-revision", Interactions: interaction.State{Paused: tc.paused}}, snapshotErr: tc.readErr, controlErr: tc.controlErr}
			msg := resumeInteractionCmd(ws, "selected-chat")().(interactionResumedMsg)
			require.Equal(t, "selected-chat", msg.sessionID)
			require.Equal(t, tc.paused && tc.readErr == nil && tc.controlErr == nil, msg.resumed)
			if tc.paused && tc.readErr == nil {
				require.Equal(t, []engineering.WorkflowControl{{Action: "interaction_resume", ExpectedRevision: "current-revision"}}, ws.controls)
			} else {
				require.Empty(t, ws.controls)
			}
			if tc.readErr != nil {
				require.ErrorIs(t, msg.err, tc.readErr)
			} else {
				require.ErrorIs(t, msg.err, tc.controlErr)
			}
		})
	}
}

func TestLateResumeDoesNotSendPromptIntoAnotherConversation(t *testing.T) {
	t.Parallel()
	m, _ := newRemoteLifecycleUI()
	m.session = &session.Session{ID: "new-chat"}
	require.NotPanics(t, func() {
		cmd := m.handleInteractionResumed(interactionResumedMsg{sessionID: "old-chat", resumed: true})
		require.NotNil(t, cmd())
	})
}

func TestResumeUseCarriesPendingStepAndCancellation(t *testing.T) {
	t.Parallel()
	ws := &interactionResumeWorkspace{snapshot: engineering.WorkflowSnapshot{Revision: "rev", Interactions: interaction.State{Paused: true, Reason: "Approve the sign-in on your phone", Canceled: true}}}
	msg := resumeInteractionCmd(ws, "chat")().(interactionResumedMsg)
	require.True(t, msg.resumed)
	require.True(t, msg.canceled)
	require.Equal(t, "Approve the sign-in on your phone", msg.reason)
	require.Equal(t, []string{"chat", "chat"}, ws.ids, "only the selected conversation is read and resumed")
}

func TestResumeContinuationDoesNotClaimTheStepSucceeded(t *testing.T) {
	t.Parallel()
	identity := func(s string) string { return s }
	prompt := resumeContinuationPrompt(identity, "Approve the sign-in on your phone")
	require.Contains(t, prompt, "Approve the sign-in on your phone")
	require.Contains(t, prompt, "fresh observation")
	require.Contains(t, prompt, "actually complete")
	require.NotContains(t, prompt, "I have completed")
}

func TestCanceledHandoffResumeDoesNotRestartTask(t *testing.T) {
	t.Parallel()
	m, _ := newRemoteLifecycleUI()
	m.session = &session.Session{ID: "chat"}
	cmd := m.handleInteractionResumed(interactionResumedMsg{sessionID: "chat", resumed: true, canceled: true})
	require.NotNil(t, cmd)
	msg := cmd()
	info, ok := msg.(util.InfoMsg)
	require.True(t, ok, "a canceled task only reports that control returned, got %T", msg)
	require.Contains(t, info.Msg, "will not restart")
}
