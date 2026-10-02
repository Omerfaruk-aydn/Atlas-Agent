package shell

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRecipesArgvLiteralExecution(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	s := NewShell(&Options{WorkingDir: root})
	literal := "$(touch escaped) ; echo injected ' \" $HOME\nnext"
	stdout, stderr, err := s.ExecArgv(t.Context(), []string{"printf", "%s", literal})
	require.NoError(t, err, stderr)
	require.Equal(t, literal, stdout)
	_, err = os.Stat(filepath.Join(root, "escaped"))
	require.True(t, os.IsNotExist(err))
	s.SetBlockFuncs([]BlockFunc{func(args []string) bool { return len(args) > 0 && args[0] == "printf" }})
	_, _, err = s.ExecArgv(t.Context(), []string{"printf", "%s", "blocked"})
	require.Error(t, err, "literal commands retain policy checks, including builtins")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, _, err = s.ExecArgv(ctx, []string{"printf", "%s", "canceled"})
	require.ErrorIs(t, err, context.Canceled)
	executable, err := os.Executable()
	require.NoError(t, err)
	s.SetBlockFuncs(nil)
	stdout, stderr, err = s.ExecArgv(t.Context(), []string{executable, "-test.run=^TestRecipesArgvChildProcess$", "--", "--atlas-argv-fixture", literal})
	require.NoError(t, err, stderr)
	var observed []string
	require.NoError(t, json.Unmarshal([]byte(stdout), &observed))
	require.Equal(t, []string{literal}, observed, "an actual child process receives one unchanged argument")
}

func TestRecipesArgvChildProcess(t *testing.T) {
	for i, arg := range os.Args {
		if arg == "--atlas-argv-fixture" {
			_ = json.NewEncoder(os.Stdout).Encode(os.Args[i+1:])
			os.Exit(0)
		}
	}
}
