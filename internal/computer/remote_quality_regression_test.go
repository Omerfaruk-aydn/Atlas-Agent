package computer

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRemoteQualityCanceledLaunchDoesNotDispatch(t *testing.T) {
	f := &fakeLauncher{pid: 1}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, _ = LaunchAndVerify(ctx, f, LaunchPlan{}, time.Millisecond)
	require.Empty(t, f.launched, "cancellation before the request must not start a process")
}

func TestRemoteQualityUnrelatedWindowDoesNotVerifyStartApp(t *testing.T) {
	w, ok := launchedWindow([]AppWindowObservation{{ID: "x", ProcessName: "unrelated.exe", ProcessID: 999}}, map[string]bool{}, "", "", 0)
	require.False(t, ok, "an unrelated new window must not verify a Start menu app: %v", w)
}
