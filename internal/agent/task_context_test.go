package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/codegraph"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
	"github.com/stretchr/testify/require"
)

func TestTaskContextMainAndDispatch(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("Use focused verification."), 0o644))
	a := &sessionAgent{workingDir: root, engineering: engineering.NewStore(t.TempDir())}
	text, err := a.deliveryContext(t.Context(), "main", "Implement feature", []session.Todo{{ID: "slice", Content: "Implement feature", Status: session.TodoStatusInProgress, OwnedPaths: []string{"."}}})
	require.NoError(t, err)
	require.Contains(t, text, "<task_context>")
	require.Contains(t, text, "Use focused verification.")
}

func TestTaskContextScopedInstructionsAndUnicode(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "feature"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("Root guidance."), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "feature", "AGENTS.md"), []byte("Scoped guidance. "+strings.Repeat("ğ", 3000)), 0o644))
	for i := range 20 {
		require.NoError(t, os.WriteFile(filepath.Join(root, "feature", string(rune('a'+i))+".go"), []byte("package feature\n// "+strings.Repeat("ö", 3000)), 0o644))
	}
	cfg := config.NewTestStore(&config.Config{}).Scoped(root)
	c := &coordinator{cfg: cfg, engineering: engineering.NewStore(t.TempDir())}
	task := session.Todo{ID: "feature", OwnedPaths: []string{"feature"}, AcceptanceCriteria: []string{"Works"}}
	packet, err := c.buildTaskContext(t.Context(), task, codegraph.CodeGraph{})
	require.NoError(t, err)
	require.Equal(t, session.TaskFingerprint(task), packet.TaskFingerprint)
	require.True(t, packet.Truncated)
	require.LessOrEqual(t, len(packet.Sources), 16)
	require.Contains(t, packet.Sources[0].Content, "Root guidance")
	require.Contains(t, packet.Sources[1].Content, "Scoped guidance")
	data, err := json.Marshal(packet)
	require.NoError(t, err)
	require.LessOrEqual(t, len(data), 24*1024)
	require.True(t, utf8.Valid(data))
}

func TestTaskContextStaleAndForeignRoot(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "feature.go"), []byte("package feature\n"), 0o644))
	c := &coordinator{cfg: config.NewTestStore(&config.Config{}).Scoped(root), engineering: engineering.NewStore(t.TempDir())}
	task := session.Todo{ID: "slice", OwnedPaths: []string{"feature.go"}}
	_, err := c.buildTaskContext(t.Context(), task, codegraph.CodeGraph{Root: t.TempDir()})
	require.Error(t, err)
	packet, err := c.buildTaskContext(t.Context(), task, codegraph.CodeGraph{Root: root, SourceFingerprint: "stale"})
	require.NoError(t, err)
	require.NotEmpty(t, packet.Gaps)
	require.Equal(t, engineering.Hash("package feature\n"), packet.Sources[0].Hash)
	task.OwnedPaths = []string{"../foreign.go"}
	_, err = c.buildTaskContext(t.Context(), task, codegraph.CodeGraph{})
	require.Error(t, err)
}
