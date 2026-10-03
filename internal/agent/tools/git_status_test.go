package tools

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/gitx"
	"github.com/stretchr/testify/require"
)

func runGitStatusTool(t *testing.T, workingDir string, params GitStatusParams) fantasy.ToolResponse {
	t.Helper()
	tool := NewGitStatusTool(workingDir)
	input, err := json.Marshal(params)
	require.NoError(t, err)
	resp, err := tool.Run(context.Background(), fantasy.ToolCall{
		ID:    "call-1",
		Name:  GitStatusToolName,
		Input: string(input),
	})
	require.NoError(t, err)
	return resp
}

// toolGitRepo builds a real repository. Testing a git wrapper against
// anything but real git output tests the fixture, not the wrapper.
func toolGitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "--initial-branch=main"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "Test"},
		{"config", "commit.gpgsign", "false"},
		{"config", "core.autocrlf", "false"},
	} {
		_, err := gitx.Run(context.Background(), dir, args...)
		require.NoError(t, err)
	}
	return dir
}

func toolGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	_, err := gitx.Run(context.Background(), dir, args...)
	require.NoError(t, err)
}

func toolCommit(t *testing.T, dir, message string) {
	t.Helper()
	toolGit(t, dir, "add", "-A")
	toolGit(t, dir, "commit", "-m", message)
}

func TestGitStatusToolReportsACleanTree(t *testing.T) {
	dir := toolGitRepo(t)
	writeDeadCodeFile(t, dir, "a.txt", "hello\n")
	toolCommit(t, dir, "init")

	resp := runGitStatusTool(t, dir, GitStatusParams{})
	require.False(t, resp.IsError)
	require.Contains(t, resp.Content, "On branch main")
	require.Contains(t, resp.Content, "Working tree clean")
}

// Staged and unstaged have to be separate sections: a file modified both
// ways would otherwise look fully captured by a commit that only takes
// half of it.
func TestGitStatusToolSeparatesStagedFromUnstaged(t *testing.T) {
	dir := toolGitRepo(t)
	writeDeadCodeFile(t, dir, "a.txt", "one\n")
	toolCommit(t, dir, "init")

	writeDeadCodeFile(t, dir, "a.txt", "two\n")
	toolGit(t, dir, "add", "a.txt")
	writeDeadCodeFile(t, dir, "b.txt", "new\n")

	resp := runGitStatusTool(t, dir, GitStatusParams{})
	require.Contains(t, resp.Content, "staged (would be committed)")
	require.Contains(t, resp.Content, "untracked")
	require.Contains(t, resp.Content, "a.txt")
	require.Contains(t, resp.Content, "b.txt")
}

// Conflicts block everything else, so they cannot be buried under the
// file list.
func TestGitStatusToolLeadsWithConflicts(t *testing.T) {
	dir := toolGitRepo(t)
	writeDeadCodeFile(t, dir, "a.txt", "base\n")
	toolCommit(t, dir, "init")

	toolGit(t, dir, "checkout", "-b", "other")
	writeDeadCodeFile(t, dir, "a.txt", "theirs\n")
	toolCommit(t, dir, "theirs")

	toolGit(t, dir, "checkout", "main")
	writeDeadCodeFile(t, dir, "a.txt", "ours\n")
	toolCommit(t, dir, "ours")

	_, _ = gitx.Run(context.Background(), dir, "merge", "other")

	resp := runGitStatusTool(t, dir, GitStatusParams{})
	require.Contains(t, resp.Content, "conflicted path(s)")
	require.Contains(t, resp.Content, "resolve these before anything else")

	var meta GitStatusResponseMetadata
	require.NoError(t, json.Unmarshal([]byte(resp.Metadata), &meta))
	require.Equal(t, 1, meta.Conflicts)
}

func TestGitStatusToolShowsRenameOrigin(t *testing.T) {
	dir := toolGitRepo(t)
	writeDeadCodeFile(t, dir, "old.txt", "content long enough to be detected as a rename\n")
	toolCommit(t, dir, "init")
	toolGit(t, dir, "mv", "old.txt", "new.txt")

	resp := runGitStatusTool(t, dir, GitStatusParams{})
	require.Contains(t, resp.Content, "new.txt (from old.txt)")
}

func TestGitStatusToolReportsDetachedHead(t *testing.T) {
	dir := toolGitRepo(t)
	writeDeadCodeFile(t, dir, "a.txt", "one\n")
	toolCommit(t, dir, "init")
	writeDeadCodeFile(t, dir, "a.txt", "two\n")
	toolCommit(t, dir, "second")
	toolGit(t, dir, "checkout", "--detach", "HEAD~1")

	resp := runGitStatusTool(t, dir, GitStatusParams{})
	require.Contains(t, resp.Content, "HEAD detached at")
}

// "Not a repository" is something the model should read and act on, not
// an error that ends the turn.
func TestGitStatusToolAnswersPlainlyOutsideARepository(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	resp := runGitStatusTool(t, t.TempDir(), GitStatusParams{})
	require.False(t, resp.IsError)
	require.Contains(t, resp.Content, "Not a git repository")
}

func TestGitStatusToolReportsUpstreamDivergence(t *testing.T) {
	origin := toolGitRepo(t)
	writeDeadCodeFile(t, origin, "a.txt", "one\n")
	toolCommit(t, origin, "init")

	clone := t.TempDir()
	_, err := gitx.Run(context.Background(), clone, "clone", origin, ".")
	require.NoError(t, err)
	toolGit(t, clone, "config", "user.email", "test@example.com")
	toolGit(t, clone, "config", "user.name", "Test")

	writeDeadCodeFile(t, clone, "b.txt", "two\n")
	toolCommit(t, clone, "local")

	resp := runGitStatusTool(t, clone, GitStatusParams{})
	require.Contains(t, resp.Content, "tracking")
	require.Contains(t, resp.Content, "1 ahead")
}

func TestGitStatusToolReportsMetadata(t *testing.T) {
	dir := toolGitRepo(t)
	writeDeadCodeFile(t, dir, "a.txt", "one\n")
	toolCommit(t, dir, "init")
	writeDeadCodeFile(t, dir, "b.txt", "two\n")

	resp := runGitStatusTool(t, dir, GitStatusParams{})
	var meta GitStatusResponseMetadata
	require.NoError(t, json.Unmarshal([]byte(resp.Metadata), &meta))
	require.Equal(t, "main", meta.Branch)
	require.False(t, meta.Clean)
	require.Equal(t, 1, meta.Unstaged)
}

func TestGitStatusToolAcceptsASubdirectory(t *testing.T) {
	dir := toolGitRepo(t)
	writeDeadCodeFile(t, dir, "sub/a.txt", "one\n")
	toolCommit(t, dir, "init")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sub", "b.txt"), []byte("x"), 0o644))

	resp := runGitStatusTool(t, dir, GitStatusParams{Path: "sub"})
	require.Contains(t, resp.Content, "On branch main")
	require.Contains(t, resp.Content, "b.txt")
}
