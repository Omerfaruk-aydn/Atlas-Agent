package tools

import (
	"context"
	"errors"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/interaction"
	"github.com/stretchr/testify/require"
)

func TestBrowserHandoffStopsSnapshotUntilUserResumes(t *testing.T) {
	const parent = "browser-handoff-parent-fixture"
	t.Cleanup(func() { interaction.Default.Resume(parent) })
	root := t.TempDir()
	sessions := newFakeBrowserSessions()
	ctx := engineering.WithScope(context.WithValue(t.Context(), SessionIDContextKey, "browser-handoff-child-fixture"), parent, "task")
	tool := newBrowserTool(&mockPermissionService{}, root, sessions, "fixture")
	response, err := tool.Run(ctx, fantasy.ToolCall{ID: "handoff", Input: `{"action":"handoff","text":"Complete the passkey prompt"}`})
	require.NoError(t, err)
	require.False(t, response.IsError)
	require.True(t, response.StopTurn)
	response, err = tool.Run(ctx, fantasy.ToolCall{ID: "snapshot", Input: `{"action":"snapshot"}`})
	require.NoError(t, err)
	require.False(t, response.IsError, "a user handoff is not a browser failure")
	require.True(t, response.StopTurn, "do not loop on snapshot or other browser actions")
	require.Contains(t, response.Content, "/resume-use")
	require.Contains(t, response.Metadata, parent)
	require.Empty(t, sessions.sessions, "paused calls must not launch or observe a browser")
	require.True(t, interaction.Default.Snapshot(parent).Paused)
	interaction.Default.Resume(parent)
	require.NoError(t, interaction.Default.Record(root, parent, interaction.Entry{Resource: "control", Action: "resume", Status: "ready"}))
	response, err = tool.Run(ctx, fantasy.ToolCall{ID: "resumed-snapshot", Input: `{"action":"snapshot"}`})
	require.NoError(t, err)
	require.False(t, response.IsError)
	require.False(t, response.StopTurn)
	require.Len(t, sessions.sessions, 1)
	restored := interaction.New()
	require.NoError(t, restored.Load(root, parent))
	require.False(t, restored.Snapshot(parent).Paused, "resume must survive restart")
}

func TestVisibleBrowserReportsTheConversationHoldingControl(t *testing.T) {
	const other = "browser-other-handoff-fixture"
	t.Cleanup(func() { interaction.Default.Resume(other) })
	interaction.Default.Pause(other, "manual login")
	sessions := &visibleFakeBrowserSessions{newFakeBrowserSessions()}
	ctx := context.WithValue(t.Context(), SessionIDContextKey, "browser-waiting-other-fixture")
	tool := newBrowserTool(&mockPermissionService{}, t.TempDir(), sessions, "fixture")
	response, err := tool.Run(ctx, fantasy.ToolCall{ID: "snapshot", Input: `{"action":"snapshot"}`})
	require.NoError(t, err)
	require.False(t, response.IsError)
	require.True(t, response.StopTurn)
	require.Contains(t, response.Content, other)
	require.Empty(t, sessions.sessions)
	interaction.Default.Resume(other)
	response, err = tool.Run(ctx, fantasy.ToolCall{ID: "snapshot-after-resume", Input: `{"action":"snapshot"}`})
	require.NoError(t, err)
	require.False(t, response.IsError)
	require.False(t, response.StopTurn)
}

func TestComputerHandoffStopsWithoutInput(t *testing.T) {
	const id = "computer-handoff-response-fixture"
	t.Cleanup(func() { interaction.Default.Resume(id) })
	interaction.Default.Pause(id, "manual desktop step")
	backend := &fakeComputerBackend{}
	state := &computerToolState{backend: backend}
	ctx := context.WithValue(t.Context(), SessionIDContextKey, id)
	response, err := state.runWithTimeout(ctx, "type", ComputerParams{Text: "must not be sent"})
	require.NoError(t, err)
	require.False(t, response.IsError)
	require.True(t, response.StopTurn)
	require.Empty(t, backend.typed)
	require.True(t, interaction.Default.Snapshot(id).Paused)
}

func TestInteractionDriverFailureStillReturnsAnError(t *testing.T) {
	t.Parallel()
	response := interactionAcquireResponse(errors.New("driver unavailable"))
	require.True(t, response.IsError)
	require.False(t, response.StopTurn)
}

func TestPipelineStopsAtUserHandoffBeforeNextAction(t *testing.T) {
	t.Parallel()
	calls := 0
	tool := NewToolPipeline(func(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error) {
		calls++
		return interactionAcquireResponse(&interaction.PausedError{SessionID: "paused", Reason: "manual login"}), nil
	})
	response, err := tool.Run(t.Context(), fantasy.ToolCall{ID: "paused-pipeline", Input: `{"steps":[{"id":"observe","tool":"browser","arguments":{"action":"snapshot"}},{"id":"click","tool":"browser","arguments":{"action":"click","selector":"button"}}]}`})
	require.NoError(t, err)
	require.False(t, response.IsError)
	require.True(t, response.StopTurn)
	require.Equal(t, 1, calls)
}

func TestHandoffWithoutNamedStepDoesNotPause(t *testing.T) {
	const id = "handoff-without-step-fixture"
	t.Cleanup(func() { interaction.Default.Resume(id) })
	sessions := newFakeBrowserSessions()
	ctx := context.WithValue(t.Context(), SessionIDContextKey, id)
	tool := newBrowserTool(&mockPermissionService{}, t.TempDir(), sessions, "fixture")
	response, err := tool.Run(ctx, fantasy.ToolCall{ID: "handoff", Input: `{"action":"handoff"}`})
	require.NoError(t, err)
	require.True(t, response.IsError)
	require.False(t, response.StopTurn, "an ordinary screen must not end the turn")
	require.Contains(t, response.Content, "handoff_step_required")
	require.False(t, interaction.Default.Snapshot(id).Paused)

	response, err = tool.Run(ctx, fantasy.ToolCall{ID: "handoff-named", Input: `{"action":"handoff","text":"Approve the sign-in prompt on your phone"}`})
	require.NoError(t, err)
	require.True(t, response.StopTurn)
	require.Contains(t, response.Content, "Approve the sign-in prompt on your phone")
	require.Equal(t, "Approve the sign-in prompt on your phone", interaction.Default.Snapshot(id).Reason)
}
