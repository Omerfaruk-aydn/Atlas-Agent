package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/securityaudit"
)

func runSecurityAudit(t *testing.T, root string, args ...string) (string, error) {
	t.Helper()
	command := newSecurityCommand()
	// The persistent root flags are not attached to a detached command.
	command.PersistentFlags().String("cwd", "", "")
	command.PersistentFlags().String("data-dir", "", "")
	var out bytes.Buffer
	command.SetOut(&out)
	command.SetErr(&bytes.Buffer{})
	command.SetArgs(append([]string{"audit", "--cwd", root, "--no-agent", "--skip-deps"}, args...))
	err := command.ExecuteContext(t.Context())
	return out.String(), err
}

func securityFixtureRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/x\n\ngo 1.22\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "creds.go"), []byte("package x\n\nconst apiKey = \"sk_live_9f8a7b6c5d4e3f2a1b0c\"\n"), 0o644))
	return root
}

func TestSecurityAuditReportsLeadsAsUnconfirmed(t *testing.T) {
	t.Parallel()
	root := securityFixtureRoot(t)
	out, err := runSecurityAudit(t, root)
	require.NoError(t, err)
	require.Contains(t, out, "Unconfirmed (NOT demonstrated)")
	require.Contains(t, out, "CWE-798")
	require.NotContains(t, out, "Confirmed, open")
}

func TestSecurityAuditJSON(t *testing.T) {
	t.Parallel()
	root := securityFixtureRoot(t)
	out, err := runSecurityAudit(t, root, "--json")
	require.NoError(t, err)
	var report securityaudit.Report
	require.NoError(t, json.Unmarshal([]byte(out), &report))
	require.NotEmpty(t, report.Findings)
	require.Equal(t, securityaudit.StatusUnconfirmed, report.Findings[0].Status)
}

func TestSecurityAuditFailOnIgnoresUnconfirmed(t *testing.T) {
	t.Parallel()
	root := securityFixtureRoot(t)
	_, err := runSecurityAudit(t, root, "--fail-on", "low")
	require.NoError(t, err, "an unconfirmed lead must not fail a gate")
}

func TestSecurityAuditFlagValidation(t *testing.T) {
	t.Parallel()
	root := securityFixtureRoot(t)
	_, err := runSecurityAudit(t, root, "--fail-on", "scary")
	require.ErrorContains(t, err, "unknown severity")
	_, err = runSecurityAudit(t, root, "--fix")
	require.ErrorContains(t, err, "--fix needs the agent")
}
