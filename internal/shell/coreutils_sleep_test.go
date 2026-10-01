//go:build !solaris && !illumos

package shell

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"mvdan.cc/sh/v3/expand"
	"mvdan.cc/sh/v3/interp"
	"mvdan.cc/sh/v3/syntax"
)

func runCoreutilsCommand(t *testing.T, ctx context.Context, command string) (string, error) {
	t.Helper()
	var stderr bytes.Buffer
	runner, err := interp.New(interp.StdIO(nil, nil, &stderr),
		interp.Env(expand.ListEnviron("PATH=")),
		interp.ExecHandlers(coreUtilsExecHandler))
	require.NoError(t, err)
	file, err := syntax.NewParser().Parse(strings.NewReader(command), "test")
	require.NoError(t, err)
	err = runner.Run(ctx, file)
	return stderr.String(), err
}

func TestGoCoreutilsSleepWaitsForRequestedDuration(t *testing.T) {
	t.Parallel()
	for _, command := range []string{"sleep .05", "sleep .025s .025", "sleep .001m"} {
		t.Run(command, func(t *testing.T) {
			t.Parallel()
			started := time.Now()
			stderr, err := runCoreutilsCommand(t, t.Context(), command)
			require.NoError(t, err, stderr)
			require.GreaterOrEqual(t, time.Since(started), 50*time.Millisecond)
			require.Empty(t, stderr)
		})
	}
}

func TestGoCoreutilsSleepCancellation(t *testing.T) {
	t.Parallel()
	for _, command := range []string{"sleep 60", "sleep 1h", "sleep 1d"} {
		t.Run(command, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
			defer cancel()
			started := time.Now()
			_, err := runCoreutilsCommand(t, ctx, command)
			require.True(t, errors.Is(err, context.DeadlineExceeded), "%v", err)
			require.Less(t, time.Since(started), 2*time.Second)
		})
	}
}

func TestGoCoreutilsSleepRejectsInvalidDurations(t *testing.T) {
	t.Parallel()
	for _, command := range []string{"sleep", "sleep -1", "sleep NaN", "sleep Inf", "sleep 1e100", "sleep 2x", "sleep 9000000000 9000000000"} {
		t.Run(command, func(t *testing.T) {
			t.Parallel()
			stderr, err := runCoreutilsCommand(t, t.Context(), command)
			require.Equal(t, 1, ExitCode(err))
			require.Contains(t, stderr, "sleep:")
		})
	}
}
