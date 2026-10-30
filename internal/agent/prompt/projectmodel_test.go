package prompt

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/projectmodel"
	"github.com/stretchr/testify/require"
)

func writeProject(t *testing.T, root string) {
	t.Helper()
	files := map[string]string{
		"go.mod":  "module example.com/app\n\ngo 1.22\n",
		"main.go": "package main\n\nfunc main() {}\n",
	}
	for name, content := range files {
		require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte(content), 0o644))
	}
}

func TestProjectModelIsAbsentUntilBuilt(t *testing.T) {
	workingDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	writeProject(t, workingDir)

	out := buildCoderPrompt(t, workingDir)
	require.NotContains(t, out, "<project_model>", "nothing is injected until a model is built on purpose")
}

func TestProjectModelSummaryReachesThePromptAndStaysStable(t *testing.T) {
	workingDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	writeProject(t, workingDir)

	svc := projectmodel.NewService(workingDir, filepath.Join(workingDir, ".atlas"), nil, nil)
	_, _, err := svc.Refresh(t.Context(), false)
	require.NoError(t, err)

	first := buildCoderPrompt(t, workingDir)
	require.Contains(t, first, "<project_model>")
	require.Contains(t, first, "main.go")
	require.Contains(t, first, "go test ./...")

	// The block is identical when nothing changed, so the cached prefix of
	// the system prompt stays valid.
	second := buildCoderPrompt(t, workingDir)
	require.Equal(t, first, second)

	// A change to the project is picked up when the prompt is rebuilt.
	require.NoError(t, os.MkdirAll(filepath.Join(workingDir, "cmd", "tool"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(workingDir, "cmd", "tool", "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644))
	third := buildCoderPrompt(t, workingDir)
	require.Contains(t, third, "cmd/tool/main.go")
}
