package webui

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// The composer's "/" menu lists the project's own commands from the
// Atlas server's side, and expanding one substitutes its arguments the way
// the terminal UI does.
func TestCommandsListAndExpand(t *testing.T) {
	h := newHarness(t)
	dir := filepath.Join(h.project, ".atlas", "commands")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "review.md"), []byte("Review $FILE and list problems.\n"), 0o644))

	b := h.signIn()
	ws := b.open(h.project)

	var list []slashCommand
	require.Equal(t, http.StatusOK, b.json(http.MethodGet, wsPath(ws.ID, "/commands"), nil, &list))
	var found *slashCommand
	for i := range list {
		if list[i].Name == "project:review" {
			found = &list[i]
		}
	}
	require.NotNil(t, found, "the project's command is offered")
	require.Equal(t, "custom", found.Kind)
	require.Len(t, found.Arguments, 1)
	require.Equal(t, "FILE", found.Arguments[0].ID)

	var out struct {
		Prompt string `json:"prompt"`
	}
	require.Equal(t, http.StatusOK, b.json(http.MethodPost, wsPath(ws.ID, "/commands/expand"), expandRequest{ID: found.ID, Args: map[string]string{"FILE": "main.go"}}, &out))
	require.Equal(t, "Review main.go and list problems.\n", out.Prompt)

	status, _ := b.raw(http.MethodPost, wsPath(ws.ID, "/commands/expand"), expandRequest{ID: "project:missing"})
	require.Equal(t, http.StatusNotFound, status)
}
