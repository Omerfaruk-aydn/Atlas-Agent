package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
)

func runSecurityVerify(t *testing.T, workingDir, input string) fantasy.ToolResponse {
	t.Helper()
	resp, err := NewSecurityVerifyTool(workingDir).Run(context.Background(), fantasy.ToolCall{
		ID: "call", Name: SecurityVerifyToolName, Input: input,
	})
	require.NoError(t, err)
	return resp
}

func TestSecurityVerifyToolNoManifest(t *testing.T) {
	t.Parallel()
	resp := runSecurityVerify(t, t.TempDir(), `{}`)
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, "write the manifest first")
}

func TestSecurityVerifyToolReportsUnprovenFindingAsUnconfirmed(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	manifest := filepath.Join(dir, ".atlas", "security", "findings.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(manifest), 0o755))
	require.NoError(t, os.WriteFile(manifest, []byte(`{"version":1,"findings":[{"id":"V1","title":"x","cwe":"CWE-22","severity":"high","status":"confirmed"}]}`), 0o644))

	resp := runSecurityVerify(t, dir, `{}`)
	require.False(t, resp.IsError)
	require.Contains(t, resp.Content, "[V1] unconfirmed")
	require.Contains(t, resp.Content, "no proof test supplied")
	require.NotContains(t, resp.Content, "[V1] confirmed")

	resp = runSecurityVerify(t, dir, `{"finding_id":"nope"}`)
	require.True(t, resp.IsError)
}
