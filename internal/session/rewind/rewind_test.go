package rewind

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/db"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/history"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/message"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
	"github.com/stretchr/testify/require"
)

type harness struct {
	sessions session.Service
	messages message.Service
	files    history.Service
	rewind   Service
}

func newHarness(t *testing.T) harness {
	dataDir := t.TempDir()
	t.Cleanup(func() {
		require.NoError(t, db.Release(dataDir))
		db.ResetPool()
	})
	conn, err := db.Connect(t.Context(), dataDir)
	require.NoError(t, err)
	h := harness{
		sessions: session.NewService(db.New(conn), conn),
		messages: message.NewService(db.New(conn)),
		files:    history.NewService(db.New(conn), conn),
	}
	h.rewind = NewService(h.sessions, h.messages, h.files)
	return h
}

func (h harness) say(t *testing.T, sessionID string, role message.MessageRole, text string) message.Message {
	m, err := h.messages.Create(t.Context(), sessionID, message.CreateMessageParams{
		Role:  role,
		Parts: []message.ContentPart{message.TextContent{Text: text}},
	})
	require.NoError(t, err)
	return m
}

func texts(t *testing.T, h harness, sessionID string) []string {
	msgs, err := h.messages.List(t.Context(), sessionID)
	require.NoError(t, err)
	out := make([]string, len(msgs))
	for i, m := range msgs {
		out[i] = m.Content().Text
	}
	return out
}

func readFile(t *testing.T, path string) string {
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(data)
}

// The usual shape of a session: each user message is answered by an
// assistant message whose tools change files.
func TestForkAtGoesBackToJustBeforeTheMessage(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()
	workDir := t.TempDir()
	aPath := filepath.Join(workDir, "a.txt")
	bPath := filepath.Join(workDir, "b.txt")

	src, err := h.sessions.Create(ctx, "original")
	require.NoError(t, err)
	h.say(t, src.ID, message.User, "edit a")
	a1 := h.say(t, src.ID, message.Assistant, "edited a")
	_, err = h.files.Create(ctx, src.ID, aPath, "baseline-a", "")
	require.NoError(t, err)
	_, err = h.files.CreateVersion(ctx, src.ID, aPath, "a-after-1", a1.ID)
	require.NoError(t, err)

	u2 := h.say(t, src.ID, message.User, "create b and edit a again")
	a2 := h.say(t, src.ID, message.Assistant, "done")
	_, err = h.files.Create(ctx, src.ID, bPath, "", a2.ID)
	require.NoError(t, err)
	_, err = h.files.CreateVersion(ctx, src.ID, bPath, "b-after-2", a2.ID)
	require.NoError(t, err)
	_, err = h.files.CreateVersion(ctx, src.ID, aPath, "a-after-2", a2.ID)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(aPath, []byte("a-after-2"), 0o644))
	require.NoError(t, os.WriteFile(bPath, []byte("b-after-2"), 0o644))

	written, deleted, err := h.rewind.Preview(ctx, src.ID, u2.ID)
	require.NoError(t, err)
	require.Equal(t, [2]int{1, 1}, [2]int{written, deleted})

	result, err := h.rewind.ForkAt(ctx, src.ID, u2.ID)
	require.NoError(t, err)

	require.Equal(t, []string{"edit a", "edited a"}, texts(t, h, result.Session.ID), "the fork ends with the answer before the chosen message")
	require.Equal(t, "a-after-1", readFile(t, aPath))
	_, err = os.Stat(bPath)
	require.True(t, os.IsNotExist(err), "b.txt was created by the chosen turn")
	require.Equal(t, 1, result.FilesWritten)
	require.Equal(t, 1, result.FilesDeleted)

	require.Len(t, texts(t, h, src.ID), 4, "the source session is untouched")
	require.Empty(t, result.Session.ParentSessionID)
	listed, err := h.sessions.List(ctx)
	require.NoError(t, err)
	ids := make([]string, len(listed))
	for i, s := range listed {
		ids[i] = s.ID
	}
	require.Contains(t, ids, result.Session.ID, "the fork shows up in the session list")

	written, deleted, err = h.rewind.Preview(ctx, src.ID, u2.ID)
	require.NoError(t, err)
	require.Zero(t, written+deleted, "files already in their restored state are not counted")
}

func TestForkOfAForkStillRestoresFiles(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()
	aPath := filepath.Join(t.TempDir(), "a.txt")

	src, err := h.sessions.Create(ctx, "original")
	require.NoError(t, err)
	u1 := h.say(t, src.ID, message.User, "first")
	a1 := h.say(t, src.ID, message.Assistant, "one")
	_, err = h.files.Create(ctx, src.ID, aPath, "baseline", "")
	require.NoError(t, err)
	_, err = h.files.CreateVersion(ctx, src.ID, aPath, "v1", a1.ID)
	require.NoError(t, err)
	h.say(t, src.ID, message.User, "second")
	a2 := h.say(t, src.ID, message.Assistant, "two")
	_, err = h.files.CreateVersion(ctx, src.ID, aPath, "v2", a2.ID)
	require.NoError(t, err)
	u3 := h.say(t, src.ID, message.User, "third")
	require.NoError(t, os.WriteFile(aPath, []byte("v2"), 0o644))

	first, err := h.rewind.ForkAt(ctx, src.ID, u3.ID)
	require.NoError(t, err)
	require.Equal(t, "v2", readFile(t, aPath))

	copied, err := h.messages.List(ctx, first.Session.ID)
	require.NoError(t, err)
	require.Equal(t, "second", copied[2].Content().Text)
	_, err = h.rewind.ForkAt(ctx, first.Session.ID, copied[2].ID)
	require.NoError(t, err)
	require.Equal(t, "v1", readFile(t, aPath), "the fork carries the file history")

	_, err = h.rewind.ForkAt(ctx, first.Session.ID, copied[0].ID)
	require.NoError(t, err)
	require.Equal(t, "baseline", readFile(t, aPath))
	_ = u1
}

func TestForkKeepsTheSummaryPointer(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()

	src, err := h.sessions.Create(ctx, "compacted")
	require.NoError(t, err)
	h.say(t, src.ID, message.User, "old work")
	h.say(t, src.ID, message.Assistant, "old answer")
	summary, err := h.messages.Create(ctx, src.ID, message.CreateMessageParams{
		Role:             message.Assistant,
		Parts:            []message.ContentPart{message.TextContent{Text: "summary"}},
		IsSummaryMessage: true,
	})
	require.NoError(t, err)
	src.SummaryMessageID = summary.ID
	_, err = h.sessions.Save(ctx, src)
	require.NoError(t, err)
	h.say(t, src.ID, message.User, "next")
	h.say(t, src.ID, message.Assistant, "next answer")
	u := h.say(t, src.ID, message.User, "latest")

	result, err := h.rewind.ForkAt(ctx, src.ID, u.ID)
	require.NoError(t, err)
	fork, err := h.sessions.Get(ctx, result.Session.ID)
	require.NoError(t, err)
	require.NotEmpty(t, fork.SummaryMessageID)
	got, err := h.messages.Get(ctx, fork.SummaryMessageID)
	require.NoError(t, err)
	require.Equal(t, "summary", got.Content().Text)
	require.Equal(t, fork.ID, got.SessionID)
}

// Copied messages are all created within the same second, the resolution
// of created_at; they must still come back in order.
func TestForkPreservesMessageOrder(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()

	src, err := h.sessions.Create(ctx, "long")
	require.NoError(t, err)
	var want []string
	for i := range 20 {
		role := message.User
		if i%2 == 1 {
			role = message.Assistant
		}
		text := fmt.Sprintf("message %02d", i)
		h.say(t, src.ID, role, text)
		want = append(want, text)
	}
	last := h.say(t, src.ID, message.User, "last")

	require.Equal(t, append(want, "last"), texts(t, h, src.ID))
	result, err := h.rewind.ForkAt(ctx, src.ID, last.ID)
	require.NoError(t, err)
	require.Equal(t, want, texts(t, h, result.Session.ID))
}
