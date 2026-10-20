package backend

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/app"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/proto"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/question"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestSendMessage_RetriedRunIDStartsOneRun(t *testing.T) {
	t.Parallel()
	b, _ := newTestBackend(t)
	coord := newBlockingCoordinator()
	ws := insertAgentWorkspace(t, b, coord)
	t.Cleanup(func() { close(coord.release) })

	msg := proto.AgentMessage{SessionID: "S1", Prompt: "hi", RunID: uuid.NewString()}
	require.NoError(t, b.SendMessage(ws.ID, msg))
	<-coord.entered
	require.ErrorIs(t, b.SendMessage(ws.ID, msg), ErrRunAlreadyAccepted)

	// A different RunID is a different request and runs.
	msg.RunID = uuid.NewString()
	require.NoError(t, b.SendMessage(ws.ID, msg))
	require.Eventually(t, func() bool { return coord.runCount.Load() == 2 }, 5*time.Second, 10*time.Millisecond)
}

func TestClaimRunID_ExpiresAndStaysBounded(t *testing.T) {
	t.Parallel()
	ws := &Workspace{}
	now := time.Now()
	require.True(t, ws.claimRunID("a", now))
	require.False(t, ws.claimRunID("a", now.Add(time.Minute)))
	require.True(t, ws.claimRunID("a", now.Add(runIDRetention+time.Second)))

	for i := range maxRememberedRunIDs + 10 {
		ws.claimRunID(uuid.NewString(), now.Add(time.Duration(i)*time.Millisecond))
	}
	require.LessOrEqual(t, len(ws.runIDs), maxRememberedRunIDs)
}

func insertFileWorkspace(t *testing.T, b *Backend) (*Workspace, string) {
	t.Helper()
	root := t.TempDir()
	ws := insertAgentWorkspace(t, b, nil)
	ws.Path = root
	return ws, root
}

func TestWorkspaceFiles_StayInsideTheRoot(t *testing.T) {
	t.Parallel()
	b, _ := newTestBackend(t)
	ws, root := insertFileWorkspace(t, b)
	outside := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "ok.txt"), []byte("ok"), 0o600))

	for _, p := range []string{"../secret.txt", "..", "/etc/passwd", filepath.Join(outside, "secret.txt"), "sub/../../secret.txt", `C:\Windows\win.ini`} {
		_, err := b.ReadWorkspaceFile(ws.ID, p, 0)
		require.Error(t, err, p)
		require.NotContains(t, err.Error(), "secret", p)
	}
	_, err := b.ListWorkspaceDirectory(ws.ID, "..")
	require.ErrorIs(t, err, ErrPathOutsideWorkspace)

	// A symlink pointing out of the workspace is neither readable nor
	// listed. Creating one needs privileges Windows may not grant.
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(root, "link.txt")); err == nil {
		_, err := b.ReadWorkspaceFile(ws.ID, "link.txt", 0)
		require.ErrorIs(t, err, ErrPathOutsideWorkspace)
		listing, err := b.ListWorkspaceDirectory(ws.ID, "")
		require.NoError(t, err)
		for _, e := range listing.Entries {
			require.NotEqual(t, "link.txt", e.Name)
		}
	}

	got, err := b.ReadWorkspaceFile(ws.ID, "ok.txt", 0)
	require.NoError(t, err)
	require.Equal(t, "ok", got.Content)
}

func TestListWorkspaceDirectory_DirsFirstAndIgnored(t *testing.T) {
	t.Parallel()
	b, _ := newTestBackend(t)
	ws, root := insertFileWorkspace(t, b)
	require.NoError(t, os.WriteFile(filepath.Join(root, ".gitignore"), []byte("build/\n*.log\n"), 0o600))
	for _, d := range []string{"src", "build", "node_modules"} {
		require.NoError(t, os.MkdirAll(filepath.Join(root, d), 0o755))
	}
	for _, f := range []string{"b.go", "A.md", "debug.log", "src/main.go"} {
		require.NoError(t, os.WriteFile(filepath.Join(root, f), []byte("x"), 0o600))
	}

	listing, err := b.ListWorkspaceDirectory(ws.ID, "")
	require.NoError(t, err)
	var names []string
	for _, e := range listing.Entries {
		names = append(names, e.Name)
	}
	require.Equal(t, []string{"src", ".gitignore", "A.md", "b.go"}, names)

	sub, err := b.ListWorkspaceDirectory(ws.ID, "src")
	require.NoError(t, err)
	require.Len(t, sub.Entries, 1)
	require.Equal(t, "src/main.go", sub.Entries[0].Path)

	_, err = b.ListWorkspaceDirectory(ws.ID, "b.go")
	require.ErrorIs(t, err, ErrNotADirectory)
}

func TestReadWorkspaceFile_TruncatesButHashesWholeFile(t *testing.T) {
	t.Parallel()
	b, _ := newTestBackend(t)
	ws, root := insertFileWorkspace(t, b)
	body := strings.Repeat("0123456789", 100)
	require.NoError(t, os.WriteFile(filepath.Join(root, "big.txt"), []byte(body), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "bin.dat"), []byte{1, 0, 2, 3}, 0o600))

	got, err := b.ReadWorkspaceFile(ws.ID, "big.txt", 25)
	require.NoError(t, err)
	require.True(t, got.Truncated)
	require.Equal(t, body[:25], got.Content)
	sum := sha256.Sum256([]byte(body))
	require.Equal(t, hex.EncodeToString(sum[:]), got.SHA256)
	require.EqualValues(t, len(body), got.Size)

	bin, err := b.ReadWorkspaceFile(ws.ID, "bin.dat", 0)
	require.NoError(t, err)
	require.True(t, bin.Binary)
	require.Empty(t, bin.Content)

	_, err = b.ReadWorkspaceFile(ws.ID, "missing.txt", 0)
	require.ErrorIs(t, err, ErrFileNotFound)
}

func TestPendingQuestions_ListedUntilAnsweredByBatch(t *testing.T) {
	t.Parallel()
	b, _ := newTestBackend(t)
	ws := insertAgentWorkspace(t, b, nil)
	questions := question.NewService()
	ws.App = &app.App{Questions: questions}

	ask := func(id string) <-chan []question.Answer {
		out := make(chan []question.Answer, 1)
		go func() {
			answers, _ := questions.Ask(context.Background(), question.Request{
				ID:        id,
				SessionID: "S1",
				Questions: []question.Question{{ID: id + "-q", Type: question.TypeFreeText, Text: "Name?", Description: "Your name."}},
			})
			out <- answers
		}()
		return out
	}
	first := ask("batch-1")
	require.Eventually(t, func() bool {
		p, _ := b.PendingQuestions(ws.ID)
		return len(p) == 1
	}, 5*time.Second, 10*time.Millisecond)
	second := ask("batch-2")
	require.Eventually(t, func() bool {
		p, _ := b.PendingQuestions(ws.ID)
		return len(p) == 2
	}, 5*time.Second, 10*time.Millisecond)

	// Answering batch-2 by ID must not resolve batch-1, even though
	// batch-1 is the one displayed first.
	ok, err := b.AnswerQuestion(ws.ID, proto.QuestionAnswer{
		BatchRequestID: "batch-2",
		Responses:      []proto.QuestionResponse{{QuestionID: "batch-2-q", FillInText: "two"}},
	})
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "two", (<-second)[0].FillInText)

	pending, err := b.PendingQuestions(ws.ID)
	require.NoError(t, err)
	require.Len(t, pending, 1)
	require.Equal(t, "batch-1", pending[0].ID)

	// A second answer to an already-settled batch reports false.
	ok, err = b.AnswerQuestion(ws.ID, proto.QuestionAnswer{BatchRequestID: "batch-2"})
	require.NoError(t, err)
	require.False(t, ok)

	cancelled, err := b.CancelQuestion(ws.ID, "batch-1")
	require.NoError(t, err)
	require.True(t, cancelled)
	require.Nil(t, <-first)
	pending, err = b.PendingQuestions(ws.ID)
	require.NoError(t, err)
	require.Empty(t, pending)
}

func TestPendingPermissions_ListedUntilResolved(t *testing.T) {
	t.Parallel()
	b, _ := newTestBackend(t)
	ws := insertAgentWorkspace(t, b, nil)
	perms := permission.NewPermissionService(ws.Path, false, nil)
	ws.App = &app.App{Permissions: perms}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan bool, 1)
	go func() {
		granted, _ := perms.Request(ctx, permission.CreatePermissionRequest{
			SessionID: "S1", ToolCallID: "tc1", ToolName: "write", Action: "write", Path: ws.Path,
		})
		result <- granted
	}()

	var pending []permission.PermissionRequest
	require.Eventually(t, func() bool {
		pending, _ = b.PendingPermissions(ws.ID)
		return len(pending) == 1
	}, 5*time.Second, 10*time.Millisecond)
	require.Equal(t, "tc1", pending[0].ToolCallID)

	ok, err := b.GrantPermission(ws.ID, proto.PermissionGrant{
		Permission: proto.PermissionRequest{ID: pending[0].ID, ToolCallID: "tc1"},
		Action:     proto.PermissionAllow,
	})
	require.NoError(t, err)
	require.True(t, ok)
	require.True(t, <-result)

	pending, err = b.PendingPermissions(ws.ID)
	require.NoError(t, err)
	require.Empty(t, pending)
}

func TestPendingPermissions_CancelledRunIsNotPending(t *testing.T) {
	t.Parallel()
	b, _ := newTestBackend(t)
	ws := insertAgentWorkspace(t, b, nil)
	perms := permission.NewPermissionService(ws.Path, false, nil)
	ws.App = &app.App{Permissions: perms}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_, _ = perms.Request(ctx, permission.CreatePermissionRequest{
			SessionID: "S1", ToolCallID: "tc1", ToolName: "write", Action: "write", Path: ws.Path,
		})
		close(done)
	}()
	require.Eventually(t, func() bool {
		p, _ := b.PendingPermissions(ws.ID)
		return len(p) == 1
	}, 5*time.Second, 10*time.Millisecond)
	cancel()
	<-done
	pending, err := b.PendingPermissions(ws.ID)
	require.NoError(t, err)
	require.Empty(t, pending)
}

func TestPermissionMode_SetAndValidate(t *testing.T) {
	t.Parallel()
	b, _ := newTestBackend(t)
	ws := insertAgentWorkspace(t, b, nil)
	ws.App = &app.App{Permissions: permission.NewPermissionService(ws.Path, false, nil)}

	mode, err := b.GetPermissionMode(ws.ID)
	require.NoError(t, err)
	require.Equal(t, permission.ModeManual, mode)

	require.NoError(t, b.SetPermissionMode(ws.ID, permission.ModePlan))
	mode, err = b.GetPermissionMode(ws.ID)
	require.NoError(t, err)
	require.Equal(t, permission.ModePlan, mode)

	require.ErrorIs(t, b.SetPermissionMode(ws.ID, "yolo"), ErrInvalidPermissionMode)
}
