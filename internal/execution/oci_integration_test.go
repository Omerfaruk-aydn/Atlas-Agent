package execution

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/stretchr/testify/require"
)

func TestOCIRealIsolation(t *testing.T) {
	path, image := os.Getenv("ATLAS_OCI_TEST_RUNTIME"), os.Getenv("ATLAS_OCI_TEST_IMAGE")
	if path == "" || image == "" {
		t.Skip("unavailable: explicit local runtime and digest fixture are not configured")
	}
	store := engineering.NewStore(t.TempDir())
	policy := ExecutionPolicy{Mode: "container-required", RuntimePath: path, Image: image, ReadOnly: true, TimeoutMS: 30000}
	runner, err := NewRunner(t.Context(), policy, store)
	require.NoError(t, err)
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, ".env"), []byte("HOST_SECRET=private"), 0o600))
	external := filepath.Join(t.TempDir(), "outside-project.txt")
	require.NoError(t, os.WriteFile(external, []byte("external host content"), 0o600))
	externalResult, err := runner.Run(t.Context(), Request{Root: root, Argv: []string{"/bin/sh", "-c", `test ! -e "$1"`, "atlas-probe", filepath.ToSlash(external)}})
	require.NoError(t, err)
	require.NotNil(t, externalResult.ExitCode)
	require.Zero(t, *externalResult.ExitCode)
	for _, script := range []string{
		`test ! -s /workspace/.env`,
		`test "$(ls /sys/class/net)" = lo`,
		`if printf write > /workspace/probe; then exit 1; fi`,
	} {
		result, err := runner.Run(t.Context(), Request{Root: root, Argv: []string{"/bin/sh", "-c", script}})
		require.NoError(t, err)
		require.True(t, result.Done)
		require.NotNil(t, result.ExitCode)
		require.Zero(t, *result.ExitCode)
		require.Equal(t, "linux", result.ExecutionOS)
	}
	unrelated, err := NewRunner(t.Context(), policy, engineering.NewStore(t.TempDir()))
	require.NoError(t, err)
	other, err := unrelated.Start(t.Context(), Request{Root: root, Argv: []string{"/bin/sh", "-c", "sleep 30"}})
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = unrelated.Cancel(cleanup, other.RunID)
	})
	owned, err := runner.Start(t.Context(), Request{Root: root, Argv: []string{"/bin/sh", "-c", "sleep 30"}})
	require.NoError(t, err)
	time.Sleep(300 * time.Millisecond)
	require.NoError(t, runner.Cancel(t.Context(), owned.RunID))
	result, err := runner.Observe(t.Context(), owned.RunID)
	require.NoError(t, err)
	require.True(t, result.Done)
	require.Equal(t, "cancelled", result.Status)
	require.Nil(t, result.ExitCode)
	_, _, err = runner.(*ociRunner).command(t.Context(), nil, "container", "inspect", owned.ContainerID)
	require.Error(t, err, "Owned child container must be removed")
	otherResult, err := unrelated.Observe(t.Context(), other.RunID)
	require.NoError(t, err)
	require.False(t, otherResult.Done, "Unrelated container must remain running")
}
