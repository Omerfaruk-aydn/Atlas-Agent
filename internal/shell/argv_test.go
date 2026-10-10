package shell

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

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

func TestBlockedArgvAppliesRulesToPathSpelledPrograms(t *testing.T) {
	t.Parallel()
	blocks := []BlockFunc{CommandsBlocker([]string{"curl"}), ArgumentsBlocker("go", []string{"install"}, nil)}
	require.True(t, BlockedArgv(blocks, []string{"curl", "x"}))
	require.True(t, BlockedArgv(blocks, []string{"/usr/bin/curl", "x"}))
	require.True(t, BlockedArgv(blocks, []string{"/opt/go/bin/go", "install", "x"}))
	require.False(t, BlockedArgv(blocks, []string{"/opt/go/bin/go", "test", "./..."}))
	if runtime.GOOS == "windows" {
		require.True(t, BlockedArgv(blocks, []string{`C:\Tools\CURL.EXE`, "x"}))
		require.Equal(t, "curl", ProgramName(`C:\Tools\curl.exe`))
	}
	require.Equal(t, "curl", ProgramName("curl"))
}

func TestStartArgvRunsLiteralBackgroundJobWithTimeout(t *testing.T) {
	t.Parallel()
	executable, err := os.Executable()
	require.NoError(t, err)
	manager := newBackgroundShellManager()
	literal := "a & b $(nope) 'q' \"d\" Ğü"
	job, err := manager.StartArgv(t.Context(), t.TempDir(), nil, []string{executable, "-test.run=^TestRecipesArgvChildProcess$", "--", "--atlas-argv-fixture", literal}, "fixture", time.Minute)
	require.NoError(t, err)
	job.Wait()
	stdout, stderr, done, execErr := job.GetOutput()
	require.True(t, done)
	require.NoError(t, execErr, stderr)
	var observed []string
	require.NoError(t, json.Unmarshal([]byte(stdout), &observed))
	require.Equal(t, []string{literal}, observed)
	require.False(t, job.TimedOut())

	slow, err := manager.StartArgv(t.Context(), t.TempDir(), nil, []string{executable, "-test.run=^TestArgvSleepChildProcess$", "--", "--atlas-argv-sleep"}, "sleep", 300*time.Millisecond)
	require.NoError(t, err)
	slow.Wait()
	require.True(t, slow.TimedOut(), "the job's own limit ended it")

	_, err = manager.StartArgv(t.Context(), t.TempDir(), []BlockFunc{CommandsBlocker([]string{ProgramName(executable)})}, []string{executable}, "blocked", 0)
	require.ErrorContains(t, err, "blocked by policy")
}

func TestArgvSleepChildProcess(t *testing.T) {
	for _, arg := range os.Args {
		if arg == "--atlas-argv-sleep" {
			time.Sleep(30 * time.Second)
			os.Exit(0)
		}
	}
}

func TestLimitedSyncBufferKeepsTailAndCountsTotal(t *testing.T) {
	t.Parallel()
	buffer := &syncBuffer{limit: 1024}
	for range 100 {
		_, _ = buffer.Write([]byte(strings.Repeat("x", 100)))
	}
	_, _ = buffer.Write([]byte("END"))
	require.EqualValues(t, 10003, buffer.Total())
	require.LessOrEqual(t, len(buffer.String()), 2048)
	require.True(t, strings.HasSuffix(buffer.String(), "END"))
}
