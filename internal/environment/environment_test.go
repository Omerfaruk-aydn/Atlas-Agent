package environment

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEnvironmentInspectNoExecution(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for name, content := range map[string]string{"go.mod": "module fixture\ngo 1.26.6\n", "package.json": `{"engines":{"node":">=20"},"scripts":{"postinstall":"write unexpected.txt"}}`, "package-lock.json": "{}", "requirements.txt": "fixture==1.0\n", "Cargo.toml": "[package]\nname = \"fixture\"\nrust-version = \"1.80\"\n", "Cargo.lock": "version = 4\n"} {
		require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte(content), 0o644))
	}
	plan, err := Inspect(t.Context(), root)
	require.NoError(t, err)
	require.Len(t, plan.Requirements, 4)
	require.NotEmpty(t, plan.Sources)
	require.NotEmpty(t, plan.SourceFingerprint)
	for _, command := range plan.Commands {
		if len(command.Argv) > 2 && command.Argv[1] == "-m" && command.Argv[2] == "venv" {
			require.Contains(t, command.Argv, "--copies")
		}
	}
	_, err = os.Stat(filepath.Join(root, "unexpected.txt"))
	require.True(t, os.IsNotExist(err))
}

func TestEnvironmentLockConflictAndStalePlan(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"engines":{"node":">=20"}}`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "package-lock.json"), []byte("{}"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "yarn.lock"), []byte("fixture"), 0o644))
	plan, err := Inspect(t.Context(), root)
	require.NoError(t, err)
	require.NotEmpty(t, plan.Conflicts)
	require.Empty(t, plan.Commands)
	require.NoError(t, os.Remove(filepath.Join(root, "yarn.lock")))
	plan, err = Inspect(t.Context(), root)
	require.NoError(t, err)
	require.NoError(t, ValidatePlan(t.Context(), plan))
	require.NoError(t, os.WriteFile(filepath.Join(root, "package-lock.json"), []byte(`{"changed":true}`), 0o644))
	require.Error(t, ValidatePlan(t.Context(), plan))
}

func TestEnvironmentRejectsEscapedPreparationDirectory(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "requirements.txt"), []byte("fixture==1.0\n"), 0o644))
	plan, err := Inspect(t.Context(), root)
	require.NoError(t, err)
	err = os.Symlink(t.TempDir(), filepath.Join(root, ".venv"))
	if err != nil {
		t.Skipf("Symlink fixture unavailable: %v", err)
	}
	require.Error(t, ValidatePlan(t.Context(), plan))
}

func TestEnvironmentMalformedVersionDeclarationBlocksPreparation(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "pyproject.toml"), []byte("[project]\nrequires-python = 42\n"), 0o644))
	plan, err := Inspect(t.Context(), root)
	require.NoError(t, err)
	require.NotEmpty(t, plan.Conflicts)
	require.Empty(t, plan.Commands)
}
