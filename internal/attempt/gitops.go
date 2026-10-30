package attempt

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// maxGitOutput bounds what one git call may return.
const maxGitOutput = 32 << 20

// maxSeedFile bounds an untracked file copied into a fresh worktree.
const maxSeedFile = 20 << 20

// stateExclude keeps agent bookkeeping out of captured patches.
const stateExclude = ":(exclude).atlas"

type limitedBuffer struct {
	bytes.Buffer
	exceeded bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if b.Len()+n > maxGitOutput {
		b.exceeded = true
		p = p[:max(0, maxGitOutput-b.Len())]
	}
	_, _ = b.Buffer.Write(p)
	return n, nil
}

func gitEnv() []string {
	var env []string
	for _, v := range os.Environ() {
		switch {
		case strings.HasPrefix(v, "GIT_DIR="),
			strings.HasPrefix(v, "GIT_WORK_TREE="),
			strings.HasPrefix(v, "GIT_INDEX_FILE="),
			strings.HasPrefix(v, "GIT_COMMON_DIR="):
			continue
		}
		env = append(env, v)
	}
	return append(env, "GIT_TERMINAL_PROMPT=0", "GIT_PAGER=cat", "LC_ALL=C")
}

// git runs git in dir with optional stdin and returns stdout.
func git(ctx context.Context, dir string, stdin []byte, args ...string) (string, error) {
	full := append([]string{"-c", "core.longpaths=true", "-c", "core.quotepath=off", "-C", dir}, args...)
	cmd := exec.CommandContext(ctx, "git", full...)
	cmd.Env = gitEnv()
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stdout, stderr limitedBuffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if stdout.exceeded {
		return "", errors.New("git output exceeds 32 MiB")
	}
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return stdout.String(), fmt.Errorf("git %s: %s", args[0], clip(msg, 600))
	}
	return stdout.String(), nil
}

// snapshot is the starting state every attempt is seeded from.
type snapshot struct {
	Head      string
	Patch     []byte
	Untracked []string
}

// takeSnapshot records HEAD, the uncommitted tracked changes and the
// untracked, non-ignored files under root.
func takeSnapshot(ctx context.Context, root string) (snapshot, error) {
	head, err := git(ctx, root, nil, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return snapshot{}, fmt.Errorf("the repository needs at least one commit: %w", err)
	}
	s := snapshot{Head: strings.TrimSpace(head)}
	patch, err := git(ctx, root, nil, "diff", "--binary", "--no-ext-diff", "HEAD", "--", ".", stateExclude)
	if err != nil {
		return snapshot{}, err
	}
	s.Patch = []byte(patch)
	list, err := git(ctx, root, nil, "ls-files", "--others", "--exclude-standard", "-z", "--", ".", stateExclude)
	if err != nil {
		return snapshot{}, err
	}
	for _, p := range strings.Split(list, "\x00") {
		if p != "" {
			s.Untracked = append(s.Untracked, p)
		}
	}
	return s, nil
}

// worktree is a disposable checkout seeded from a snapshot.
type worktree struct {
	Root string
	Path string
	// Base is the commit the attempt's patch is computed against.
	Base string
}

// createWorktree makes a detached worktree at path holding the snapshot.
func createWorktree(ctx context.Context, root, path string, snap snapshot) (*worktree, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if _, err := git(ctx, root, nil, "worktree", "add", "--detach", path, snap.Head); err != nil {
		// An interrupted "worktree add" leaves a locked, half-made entry.
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Minute)
		defer cancel()
		_ = (&worktree{Root: root, Path: path}).remove(cleanup)
		return nil, err
	}
	w := &worktree{Root: root, Path: path, Base: snap.Head}
	if len(snap.Patch) == 0 && len(snap.Untracked) == 0 {
		return w, nil
	}
	if len(snap.Patch) > 0 {
		if _, err := git(ctx, path, snap.Patch, "apply", "--whitespace=nowarn", "-"); err != nil {
			return w, fmt.Errorf("seeding uncommitted changes: %w", err)
		}
	}
	for _, rel := range snap.Untracked {
		if err := copyUntracked(root, path, rel); err != nil {
			return w, fmt.Errorf("seeding %s: %w", rel, err)
		}
	}
	// Commit the seed so the attempt's patch contains only the attempt's
	// own work. The commit is unreferenced and never reaches a branch.
	if _, err := git(ctx, path, nil, "add", "-A", "--", ".", stateExclude); err != nil {
		return w, err
	}
	if _, err := git(ctx, path, nil,
		"-c", "user.name=atlas-attempt", "-c", "user.email=attempt@atlas.invalid",
		"-c", "commit.gpgsign=false", "commit", "--no-verify", "--allow-empty", "-q", "-m", "attempt seed"); err != nil {
		return w, err
	}
	head, err := git(ctx, path, nil, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return w, err
	}
	w.Base = strings.TrimSpace(head)
	return w, nil
}

func copyUntracked(srcRoot, dstRoot, rel string) error {
	src := filepath.Join(srcRoot, filepath.FromSlash(rel))
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() > maxSeedFile {
		return nil
	}
	dst := filepath.Join(dstRoot, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm()|0o200)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// capture stages the attempt's work and returns it as a patch against the
// seed, plus per-file statistics. Staging only touches the worktree's own
// index.
func (w *worktree) capture(ctx context.Context) (patch string, files []FileStat, err error) {
	if _, err := git(ctx, w.Path, nil, "add", "-A", "--", ".", stateExclude); err != nil {
		return "", nil, err
	}
	patch, err = git(ctx, w.Path, nil, "diff", "--cached", "--binary", "--no-ext-diff", "--no-textconv", w.Base, "--")
	if err != nil {
		return "", nil, err
	}
	numstat, err := git(ctx, w.Path, nil, "diff", "--cached", "--numstat", "--no-ext-diff", "--no-textconv", w.Base, "--")
	if err != nil {
		return "", nil, err
	}
	return patch, parseNumstat(numstat), nil
}

// parseNumstat reads "git diff --numstat" output.
func parseNumstat(out string) []FileStat {
	var files []FileStat
	for _, line := range strings.Split(out, "\n") {
		parts := strings.SplitN(strings.TrimRight(line, "\r"), "\t", 3)
		if len(parts) != 3 {
			continue
		}
		f := FileStat{Path: parts[2]}
		if parts[0] == "-" || parts[1] == "-" {
			f.Binary = true
		} else {
			f.Additions, _ = strconv.Atoi(parts[0])
			f.Deletions, _ = strconv.Atoi(parts[1])
		}
		files = append(files, f)
	}
	return files
}

// remove deletes the worktree and its registration.
func (w *worktree) remove(ctx context.Context) error {
	_, err := git(ctx, w.Root, nil, "worktree", "remove", "--force", "--force", w.Path)
	if err != nil {
		// Fall back to deleting the directory and pruning the stale entry.
		if rmErr := os.RemoveAll(w.Path); rmErr != nil {
			return fmt.Errorf("%w; removing directory: %v", err, rmErr)
		}
		_, _ = git(ctx, w.Root, nil, "worktree", "prune")
	}
	return nil
}

// patchApplies reports whether patch applies cleanly to the tree at root.
func patchApplies(ctx context.Context, root string, patch []byte) error {
	_, err := git(ctx, root, patch, "apply", "--check", "--whitespace=nowarn", "-")
	return err
}

// applyPatch applies patch to the tree at root after checking it.
func applyPatch(ctx context.Context, root string, patch []byte) error {
	if err := patchApplies(ctx, root, patch); err != nil {
		return err
	}
	_, err := git(ctx, root, patch, "apply", "--whitespace=nowarn", "-")
	return err
}

func clip(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	return s[:limit] + "..."
}
