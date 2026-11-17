package tools

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDesignRequirementReadsTheSavedFile(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dir := filepath.Join(root, ".atlas", "design", "files")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	write := func(body string) {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "demo.json"), []byte(body), 0o644))
	}
	req := browserRequirement{ID: "r", Verify: "design", Subject: "demo", Expect: "COP31 Companion > 01 Today; Tab bar"}
	task := &browserTask{root: root, Requirements: []browserRequirement{req}}

	status, _ := task.effective(req)
	require.Equal(t, ReqPending, status, "no file yet")

	write(`{"pages":[{"children":[{"name":"COP31 Companion","children":[]},{"name":"01 Today"},{"name":"Tab bar"}]}]}`)
	status, _ = task.effective(req)
	require.Equal(t, ReqPending, status, "the frame is a sibling of the section")

	write(`{"pages":[{"children":[{"name":"COP31 Companion","children":[{"name":"01 Today"}]},{"name":"Tab bar"}]}]}`)
	status, _ = task.effective(req)
	require.Equal(t, ReqVerified, status)

	blocked := req
	blocked.Status = ReqBlocked
	status, _ = task.effective(blocked)
	require.Equal(t, ReqBlocked, status)
}

func TestDesignRequirementIsValidatedAndNotSelfVerified(t *testing.T) {
	t.Parallel()
	require.Error(t, validRequirements([]BrowserRequirementInput{{ID: "a", Text: "x", Verify: "design", Expect: "Tab bar"}}))
	require.Error(t, validRequirements([]BrowserRequirementInput{{ID: "a", Text: "x", Verify: "design", Subject: "../x", Expect: "Tab bar"}}))
	require.NoError(t, validRequirements([]BrowserRequirementInput{{ID: "a", Text: "x", Verify: "design", Subject: "demo", Expect: "Tab bar"}}))
	require.False(t, evidenceMatches("design", "visual"))
}
