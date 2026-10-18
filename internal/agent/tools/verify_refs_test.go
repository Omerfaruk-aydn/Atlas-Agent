package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolvePlanReferencesReplacesIndexes(t *testing.T) {
	t.Parallel()
	plan := []VerificationStep{
		{Name: "build", Tool: "bash", Input: json.RawMessage(`{"command":"npm run build"}`)},
		{Name: "test", Tool: "bash", Input: json.RawMessage(`{"command":"npm test"}`)},
	}
	checks := []VerificationStep{
		{Name: "", Tool: "bash", Input: json.RawMessage(`[1]`)},
		{Name: "first", Input: json.RawMessage(`0`)},
		{Name: "own", Tool: "bash", Input: json.RawMessage(`{"command":"go vet ./..."}`)},
	}
	out, err := resolvePlanReferences(checks, plan)
	require.NoError(t, err)
	require.Equal(t, "test", out[0].Name)
	require.JSONEq(t, `{"command":"npm test"}`, string(out[0].Input))
	require.Equal(t, "first", out[1].Name)
	require.Equal(t, "bash", out[1].Tool)
	require.JSONEq(t, `{"command":"go vet ./..."}`, string(out[2].Input))

	_, err = resolvePlanReferences([]VerificationStep{{Name: "x", Input: json.RawMessage(`[5]`)}}, plan)
	require.ErrorContains(t, err, "plan step 5")
}

func TestDiscoverVerificationAddsRunnablePlanChecks(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".atlas"), 0o755))
	plan := `{"verify":[{"name":"unit","command":"npx vitest run"},{"command":"npm run dev"},{"command":"browser inspection at 390px"}]}`
	require.NoError(t, os.WriteFile(filepath.Join(root, ".atlas", "work.json"), []byte(plan), 0o644))
	var commands []string
	for _, step := range DiscoverVerification(root) {
		var in BashParams
		require.NoError(t, json.Unmarshal(step.Input, &in))
		commands = append(commands, in.Command)
	}
	require.Contains(t, commands, "npx vitest run")
	require.NotContains(t, commands, "npm run dev")
	require.NotContains(t, commands, "browser inspection at 390px")
}
