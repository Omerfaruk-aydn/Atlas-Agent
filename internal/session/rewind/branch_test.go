package rewind

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/message"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
	"github.com/stretchr/testify/require"
)

func (h harness) toolTurn(t *testing.T, sessionID, name string) (call, result message.Message) {
	call, err := h.messages.Create(t.Context(), sessionID, message.CreateMessageParams{
		Role:  message.Assistant,
		Parts: []message.ContentPart{message.ToolCall{ID: name + "-id", Name: name, Finished: true}},
	})
	require.NoError(t, err)
	result, err = h.messages.Create(t.Context(), sessionID, message.CreateMessageParams{
		Role:  message.Tool,
		Parts: []message.ContentPart{message.ToolResult{ToolCallID: name + "-id", Name: name, Content: "ok"}},
	})
	require.NoError(t, err)
	return call, result
}

func TestBranchAtAssistantStepKeepsFilesAndTagsOrigin(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()
	aPath := filepath.Join(t.TempDir(), "a.txt")

	src, err := h.sessions.Create(ctx, "original")
	require.NoError(t, err)
	h.say(t, src.ID, message.User, "go")
	call, result := h.toolTurn(t, src.ID, "edit")
	_, err = h.files.Create(ctx, src.ID, aPath, "base", "")
	require.NoError(t, err)
	_, err = h.files.CreateVersion(ctx, src.ID, aPath, "after-edit", call.ID)
	require.NoError(t, err)
	h.say(t, src.ID, message.Assistant, "finished")

	b := NewBrancher(h.sessions, h.messages, h.files)
	res, err := b.Branch(ctx, BranchOptions{SourceID: src.ID, MessageID: result.ID})
	require.NoError(t, err)
	require.Equal(t, 3, res.Messages)
	require.Equal(t, []string{"go", "", ""}, texts(t, h, res.Session.ID))
	require.Contains(t, res.Session.Tags, BranchTagPrefix+session.HashID(src.ID))
	require.Equal(t, "Branch: original", res.Session.Title)
	require.Zero(t, res.FilesWritten, "files are untouched unless asked")

	srcTexts := texts(t, h, src.ID)
	require.Len(t, srcTexts, 4, "the source session is never modified")

	// The branch carries the file history, so it can be rewound in turn.
	versions, err := h.files.ListBySession(ctx, res.Session.ID)
	require.NoError(t, err)
	require.Len(t, versions, 2)
}

func TestBranchRefusesUnansweredToolCall(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()
	src, err := h.sessions.Create(ctx, "s")
	require.NoError(t, err)
	h.say(t, src.ID, message.User, "go")
	call, _ := h.toolTurn(t, src.ID, "view")

	b := NewBrancher(h.sessions, h.messages, h.files)
	_, err = b.Branch(ctx, BranchOptions{SourceID: src.ID, MessageID: call.ID})
	require.ErrorContains(t, err, "unanswered")

	list, err := h.sessions.List(ctx)
	require.NoError(t, err)
	require.Len(t, list, 1, "a refused branch creates nothing")
}

func TestBranchReplaceLastUserDropsIt(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()
	src, err := h.sessions.Create(ctx, "s")
	require.NoError(t, err)
	h.say(t, src.ID, message.User, "first")
	h.say(t, src.ID, message.Assistant, "reply")
	u2 := h.say(t, src.ID, message.User, "second")
	h.say(t, src.ID, message.Assistant, "reply 2")

	b := NewBrancher(h.sessions, h.messages, h.files)
	res, err := b.Branch(ctx, BranchOptions{SourceID: src.ID, MessageID: u2.ID, ReplaceLastUser: true})
	require.NoError(t, err)
	require.True(t, res.Dropped)
	require.Equal(t, []string{"first", "reply"}, texts(t, h, res.Session.ID))

	// An assistant step is not dropped even with the flag.
	res, err = b.Branch(ctx, BranchOptions{SourceID: src.ID, MessageID: "2", ReplaceLastUser: true})
	require.NoError(t, err)
	require.False(t, res.Dropped)
	require.Len(t, texts(t, h, res.Session.ID), 2)
}

func TestBranchRestoreFilesAndDryRun(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()
	aPath := filepath.Join(t.TempDir(), "a.txt")

	src, err := h.sessions.Create(ctx, "s")
	require.NoError(t, err)
	h.say(t, src.ID, message.User, "go")
	a1 := h.say(t, src.ID, message.Assistant, "v1")
	a2 := h.say(t, src.ID, message.Assistant, "v2")
	_, err = h.files.Create(ctx, src.ID, aPath, "base", "")
	require.NoError(t, err)
	_, err = h.files.CreateVersion(ctx, src.ID, aPath, "v1", a1.ID)
	require.NoError(t, err)
	_, err = h.files.CreateVersion(ctx, src.ID, aPath, "v2", a2.ID)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(aPath, []byte("v2"), 0o644))

	b := NewBrancher(h.sessions, h.messages, h.files)
	dry, err := b.Branch(ctx, BranchOptions{SourceID: src.ID, MessageID: a1.ID, RestoreFiles: true, DryRun: true})
	require.NoError(t, err)
	require.Equal(t, 1, dry.FilesWritten)
	require.Empty(t, dry.Session.ID)
	require.Equal(t, "v2", readFile(t, aPath), "a dry run touches nothing")
	list, err := h.sessions.List(ctx)
	require.NoError(t, err)
	require.Len(t, list, 1)

	res, err := b.Branch(ctx, BranchOptions{SourceID: src.ID, MessageID: a1.ID, RestoreFiles: true})
	require.NoError(t, err)
	require.Equal(t, 1, res.FilesWritten)
	require.Equal(t, "v1", readFile(t, aPath))
}
