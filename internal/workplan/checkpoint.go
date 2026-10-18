package workplan

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// Checkpoints record the last states of the project in which the
// verification contract held ("green"). Long work breaks earlier features
// more than anything else; with a green state to compare against, a check
// that starts failing comes with the list of files changed since it last
// passed, and a feature's own check can be run against the code from before
// the feature existed to prove that it can fail at all.
//
// A checkpoint is a git commit object built from a temporary index, so the
// user's index, HEAD, branches and working tree are never touched. The
// commits are chained under refs/atlas/green to keep them from garbage
// collection. Projects that are not git repositories have no checkpoints.

const (
	// CheckpointsFile lists the recorded green states, newest last.
	CheckpointsFile = "checkpoints.json"
	// GreenRef keeps the checkpoint commits reachable.
	GreenRef = "refs/atlas/green"
	// ProbeDir holds temporary worktrees for proving checks can fail.
	ProbeDir = "probes"
	// AlternativeDir holds worktrees for trying different approaches to a
	// check that keeps failing.
	AlternativeDir = "alternatives"
	maxGreens      = 30
	gitTimeout     = 2 * time.Minute
)

// Green is one state in which the contract held.
type Green struct {
	Commit string   `json:"commit"`
	Time   int64    `json:"time"`
	Done   []string `json:"done,omitempty"`
	// Passed are the contract commands that passed in this state.
	Passed []string `json:"passed,omitempty"`
}

// Greens reads the recorded checkpoints, oldest first.
func Greens(root string) ([]Green, error) {
	data, err := os.ReadFile(filepath.Join(root, Dir, CheckpointsFile))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var greens []Green
	if err := json.Unmarshal(data, &greens); err != nil {
		return nil, fmt.Errorf("%s/%s: %w", Dir, CheckpointsFile, err)
	}
	return greens, nil
}

// IsGitRepo reports whether root is inside a git work tree.
func IsGitRepo(ctx context.Context, root string) bool {
	out, err := git(ctx, root, nil, "rev-parse", "--is-inside-work-tree")
	return err == nil && strings.TrimSpace(out) == "true"
}

// Snapshot records the working tree, tracked and untracked files alike
// (ignored files excluded), as a commit object and returns its id. It
// works on a copy of the index, so nothing the user can see changes.
func Snapshot(ctx context.Context, root, message string) (string, error) {
	gitDir, err := git(ctx, root, nil, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return "", err
	}
	tmpDir, err := os.MkdirTemp("", "atlas-index-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmpDir)
	tmpPath := filepath.Join(tmpDir, "index")
	// Starting from the real index lets git reuse its stat cache instead of
	// hashing every file again. Without one (a fresh repository) the file
	// must not exist at all: git rejects an empty index file.
	if index, readErr := os.ReadFile(filepath.Join(strings.TrimSpace(gitDir), "index")); readErr == nil {
		if err := os.WriteFile(tmpPath, index, 0o600); err != nil {
			return "", err
		}
	}
	env := []string{"GIT_INDEX_FILE=" + tmpPath}
	// The plan directory is excluded even where no .gitignore covers it:
	// probe worktrees inside it must never be snapshotted as nested
	// repositories.
	if _, err := git(ctx, root, env, "add", "-A", "--", ".", ":(exclude)"+Dir); err != nil {
		return "", err
	}
	tree, err := git(ctx, root, env, "write-tree")
	if err != nil {
		return "", err
	}
	args := []string{"commit-tree", strings.TrimSpace(tree), "-m", message}
	if parent, err := git(ctx, root, nil, "rev-parse", "--verify", "--quiet", GreenRef); err == nil && strings.TrimSpace(parent) != "" {
		args = append(args, "-p", strings.TrimSpace(parent))
	}
	commit, err := git(ctx, root, authorEnv(), args...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(commit), nil
}

// authorEnv gives commit-tree an identity, so a repository without a
// configured user can still be checkpointed.
func authorEnv() []string {
	return []string{
		"GIT_AUTHOR_NAME=Atlas", "GIT_AUTHOR_EMAIL=atlas@localhost",
		"GIT_COMMITTER_NAME=Atlas", "GIT_COMMITTER_EMAIL=atlas@localhost",
	}
}

// RecordGreen snapshots the project as a green state with the features
// done and the commands that passed, and keeps it reachable.
func RecordGreen(ctx context.Context, root string, done, passed []string) (Green, error) {
	commit, err := Snapshot(ctx, root, "atlas: verification contract held")
	if err != nil {
		return Green{}, err
	}
	if _, err := git(ctx, root, nil, "update-ref", GreenRef, commit); err != nil {
		return Green{}, err
	}
	green := Green{Commit: commit, Time: time.Now().UnixMilli(), Done: slices.Clone(done), Passed: slices.Clone(passed)}
	greens, err := Greens(root)
	if err != nil {
		greens = nil // A corrupt list is replaced rather than blocking new checkpoints.
	}
	greens = append(greens, green)
	greens = greens[max(0, len(greens)-maxGreens):]
	data, err := json.MarshalIndent(greens, "", "  ")
	if err != nil {
		return Green{}, err
	}
	if err := os.MkdirAll(filepath.Join(root, Dir), 0o755); err != nil {
		return Green{}, err
	}
	return green, os.WriteFile(filepath.Join(root, Dir, CheckpointsFile), data, 0o644)
}

// LastGreenPassing returns the newest green state in which command passed.
func LastGreenPassing(greens []Green, command string) (Green, bool) {
	for i := len(greens) - 1; i >= 0; i-- {
		if slices.Contains(greens[i].Passed, command) {
			return greens[i], true
		}
	}
	return Green{}, false
}

// LastGreenWithout returns the newest green state in which feature was not
// yet done: the code from before the feature existed.
func LastGreenWithout(greens []Green, feature string) (Green, bool) {
	for i := len(greens) - 1; i >= 0; i-- {
		if !slices.Contains(greens[i].Done, feature) {
			return greens[i], true
		}
	}
	return Green{}, false
}

// ChangedSince lists the files that differ between commit and the current
// working tree, with a line-count summary, at most limit entries.
func ChangedSince(ctx context.Context, root, commit string, limit int) ([]string, error) {
	current, err := Snapshot(ctx, root, "atlas: comparison")
	if err != nil {
		return nil, err
	}
	out, err := git(ctx, root, nil, "diff", "--stat=120", "--stat-count="+fmt.Sprint(limit), commit, current, "--")
	if err != nil {
		return nil, err
	}
	var lines []string
	for line := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, strings.TrimSpace(line))
		}
	}
	return lines, nil
}

// AddProbe checks commit out into a temporary worktree under the plan
// directory and returns its path. Ignored files such as installed
// dependencies are not part of a checkpoint, so a check that needs them
// fails there for that reason alone; that errs on the safe side, because a
// probe only flags a check that passes without the feature.
func AddProbe(ctx context.Context, root, commit, name string) (string, error) {
	return AddWorktree(ctx, root, ProbeDir, commit, name)
}

// AddWorktree checks commit out into a detached worktree named name under
// the plan directory's sub directory and returns its path. A worktree left
// there by an earlier attempt is replaced.
func AddWorktree(ctx context.Context, root, sub, commit, name string) (string, error) {
	dir := filepath.Join(root, Dir, sub, name)
	_ = RemoveProbe(ctx, root, dir)
	if _, err := git(ctx, root, nil, "worktree", "add", "--detach", "--force", dir, commit); err != nil {
		return "", err
	}
	return dir, nil
}

// IsScratchDir reports whether dir lies in a worktree Atlas prepared under
// the plan directory, where command results say nothing about the project
// itself.
func IsScratchDir(dir string) bool {
	dir = filepath.ToSlash(dir)
	for _, sub := range []string{ProbeDir, AlternativeDir} {
		if strings.Contains(dir, Dir+"/"+sub+"/") || strings.HasPrefix(dir, Dir+"/"+sub+"/") {
			return true
		}
	}
	return false
}

// PruneProbes removes probe and alternative worktrees older than age, left
// behind when a request ended before they were judged.
func PruneProbes(ctx context.Context, root string, age time.Duration) {
	for _, sub := range []string{ProbeDir, AlternativeDir} {
		dir := filepath.Join(root, Dir, sub)
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			info, err := e.Info()
			if err != nil || !e.IsDir() || time.Since(info.ModTime()) < age {
				continue
			}
			_ = RemoveProbe(ctx, root, filepath.Join(dir, e.Name()))
		}
	}
}

// RemoveProbe deletes a probe worktree.
func RemoveProbe(ctx context.Context, root, dir string) error {
	_, err := git(ctx, root, nil, "worktree", "remove", "--force", dir)
	if err != nil {
		_ = os.RemoveAll(dir)
		_, _ = git(ctx, root, nil, "worktree", "prune")
	}
	return err
}

func git(ctx context.Context, dir string, env []string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

// KeepCommit points ref at commit so git never collects it.
func KeepCommit(ctx context.Context, root, ref, commit string) error {
	_, err := git(ctx, root, nil, "update-ref", ref, commit)
	return err
}
