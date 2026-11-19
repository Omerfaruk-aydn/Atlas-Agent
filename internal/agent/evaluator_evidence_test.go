package agent

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

func gitInTemp(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir, "-c", "user.email=t@t", "-c", "user.name=t"}, args...)...)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
}

func TestChangeEvidenceShowsDiffAndNewFiles(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	dir := t.TempDir()
	gitInTemp(t, dir, "init", "-q")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n\nfunc One() int { return 1 }\n"), 0o644))
	gitInTemp(t, dir, "add", "-A")
	gitInTemp(t, dir, "commit", "-qm", "init")

	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n\nfunc One() int { return 2 }\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "b.go"), []byte("package a\n\nfunc Two() int { return 2 }\n"), 0o644))

	// A relative path, an absolute path and a path outside the project.
	got := changeEvidence(t.Context(), dir, []string{"a.go", filepath.Join(dir, "b.go"), filepath.Join(dir, "..", "outside.go")})
	require.Contains(t, got, "--- a.go (changed)")
	require.Contains(t, got, "-func One() int { return 1 }")
	require.Contains(t, got, "+func One() int { return 2 }")
	require.Contains(t, got, "--- b.go (new file)")
	require.Contains(t, got, "func Two() int")
	require.NotContains(t, got, "outside.go")
}

func TestChangeEvidenceEmptyWithoutInput(t *testing.T) {
	t.Parallel()
	require.Empty(t, changeEvidence(t.Context(), "", []string{"a.go"}))
	require.Empty(t, changeEvidence(t.Context(), t.TempDir(), nil))
	// A file that does not exist contributes nothing.
	require.Empty(t, changeEvidence(t.Context(), t.TempDir(), []string{"missing.go"}))
}

func TestChangeEvidenceIsBounded(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	var files []string
	for i := range 8 {
		name := filepath.Join(dir, strings.Repeat("f", i+1)+".txt")
		require.NoError(t, os.WriteFile(name, []byte(strings.Repeat("line of text\n", 2000)), 0o644))
		files = append(files, name)
	}
	got := changeEvidence(t.Context(), dir, files)
	require.LessOrEqual(t, len(got), changeEvidenceMaxBytes+1024)
	require.Contains(t, got, "cut; use view for the rest")
	require.Contains(t, got, "Further changed files are not shown here")
}

func TestClipEvidenceKeepsUTF8(t *testing.T) {
	t.Parallel()
	got := clipEvidence(strings.Repeat("ç", changeEvidenceFileBytes))
	require.True(t, strings.HasSuffix(got, "\n"))
	require.NotContains(t, got, "�")
}

func TestReviewSubmitted(t *testing.T) {
	t.Parallel()
	step := func(calls ...fantasy.ToolCallContent) fantasy.StepResult {
		var content fantasy.ResponseContent
		for _, c := range calls {
			content = append(content, c)
		}
		return fantasy.StepResult{Response: fantasy.Response{Content: content}}
	}
	require.False(t, reviewSubmitted(nil))
	require.False(t, reviewSubmitted([]fantasy.StepResult{step(fantasy.ToolCallContent{ToolName: "view"})}))
	require.False(t, reviewSubmitted([]fantasy.StepResult{step(fantasy.ToolCallContent{ToolName: submitReviewToolName, Invalid: true})}), "an invalid verdict lets the reviewer correct itself")
	require.True(t, reviewSubmitted([]fantasy.StepResult{
		step(fantasy.ToolCallContent{ToolName: "view"}),
		step(fantasy.ToolCallContent{ToolName: "view"}, fantasy.ToolCallContent{ToolName: submitReviewToolName}),
	}))
	// Only the latest step counts.
	require.False(t, reviewSubmitted([]fantasy.StepResult{
		step(fantasy.ToolCallContent{ToolName: submitReviewToolName}),
		step(fantasy.ToolCallContent{ToolName: "view"}),
	}))
}
