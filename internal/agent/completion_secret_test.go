package agent

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChangedFileSecretScanSkipsOnlyDeletedFiles(t *testing.T) {
	t.Parallel()
	a, env, _ := newWorkLoopAgent(t, &scriptedModel{})
	ledger := &evaluatorLedger{root: env.workingDir, files: []string{"deleted.txt"}}
	decision, stop := a.scanChangedSecretsWithStat(ledger, func(string) (os.FileInfo, error) {
		return nil, fs.ErrNotExist
	})
	require.False(t, stop)
	require.Empty(t, decision.Unverified)
}

func TestChangedFileSecretScanFailsClosedOnStatErrors(t *testing.T) {
	t.Parallel()
	a, env, _ := newWorkLoopAgent(t, &scriptedModel{})
	ledger := &evaluatorLedger{root: env.workingDir, files: []string{"unreadable.txt"}}
	decision, stop := a.scanChangedSecretsWithStat(ledger, func(string) (os.FileInfo, error) {
		return nil, errors.New("simulated I/O failure")
	})
	require.True(t, stop)
	require.Equal(t, "Changed-file secret scan could not finish", decision.Unverified)
}

func TestChangedFileSecretScanRejectsNonRegularFiles(t *testing.T) {
	t.Parallel()
	a, env, _ := newWorkLoopAgent(t, &scriptedModel{})
	require.NoError(t, os.Mkdir(filepath.Join(env.workingDir, "changed-dir"), 0o700))
	ledger := &evaluatorLedger{root: env.workingDir, files: []string{"changed-dir"}}
	decision, stop := a.scanChangedSecrets(ledger)
	require.True(t, stop)
	require.Equal(t, "Changed-file secret scan could not finish", decision.Unverified)
}
