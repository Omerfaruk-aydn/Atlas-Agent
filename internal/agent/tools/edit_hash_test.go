package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
	"github.com/stretchr/testify/require"
)

type changingEditPermission struct {
	*mockPermissionService
	path string
}

func (p *changingEditPermission) Request(context.Context, permission.CreatePermissionRequest) (bool, error) {
	return true, os.WriteFile(p.path, []byte("user changed while approving\n"), 0o644)
}

func TestExpectedEditHashRejectsChangeDuringApproval(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")
	original := "base\r\n"
	require.NoError(t, os.WriteFile(path, []byte(original), 0o644))
	ctx := context.WithValue(t.Context(), SessionIDContextKey, "s")
	ctx = context.WithValue(ctx, expectedEditHashKey{}, engineering.Hash(original))
	edit := editContext{ctx: ctx, permissions: &changingEditPermission{mockPermissionService: &mockPermissionService{}, path: path}, files: &mockHistoryService{}, filetracker: &mockEditFileTracker{lastRead: time.Now().Add(time.Second)}, workingDir: dir}
	_, err := replaceContent(edit, path, "base", "replacement", false, fantasy.ToolCall{ID: "edit"})
	require.ErrorContains(t, err, "no write sent")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "user changed while approving\n", string(data))
}
