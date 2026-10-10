package qualityflows

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/lock"
)

type UndoPlan struct {
	ID           string            `json:"id"`
	Root         string            `json:"root"`
	Head         string            `json:"head"`
	Commits      []string          `json:"commits"`
	Paths        []string          `json:"paths"`
	LaterChanges []string          `json:"later_changes"`
	Before       map[string]string `json:"before"`
	Patch        string            `json:"patch"`
	Limitations  []string          `json:"limitations"`
}

func (p UndoPlan) Fingerprint() string {
	p.ID = ""
	data, _ := json.Marshal(p)
	return engineering.Hash(string(data))
}

func fileIdentity(root, name string) (string, error) {
	data, err := ReadFile(root, name, maxBytes)
	if os.IsNotExist(err) {
		return "absent", nil
	}
	if err != nil {
		return "", err
	}
	path, err := ProjectPath(root, name)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	return engineering.Hash(fmt.Sprintf("%o\x00%s", info.Mode().Perm(), data)), nil
}

// PrepareUndo reverses explicit reachable non-merge commits in a temporary
// index. Overlapping later changes must apply cleanly; no conflict is forced.
func PrepareUndo(ctx context.Context, root string, refs []string) (UndoPlan, error) {
	p := UndoPlan{Before: map[string]string{}, LaterChanges: []string{}, Limitations: []string{
		"Only explicitly selected committed code is removed; uncommitted feature ownership is ambiguous.",
		"Later commits touching selected paths are possible dependents, not proof of semantic dependency.",
		"Remote writes, database migrations, installed packages and external effects are not reversed.",
	}}
	if len(refs) == 0 || len(refs) > 32 {
		return p, fmt.Errorf("select 1-32 feature commits")
	}
	var err error
	p.Root, err = filepath.Abs(root)
	if err != nil {
		return p, err
	}
	p.Root, err = filepath.EvalSymlinks(p.Root)
	if err != nil {
		return p, err
	}
	data, err := git(ctx, p.Root, nil, nil, "rev-parse", "--show-toplevel")
	if err != nil {
		return p, err
	}
	canonical, err := filepath.Abs(strings.TrimSpace(string(data)))
	if err == nil {
		canonical, err = filepath.EvalSymlinks(canonical)
	}
	if err != nil || !strings.EqualFold(filepath.Clean(canonical), filepath.Clean(p.Root)) {
		return p, fmt.Errorf("run from the Git repository root")
	}
	head, err := git(ctx, p.Root, nil, nil, "rev-parse", "HEAD")
	if err != nil {
		return p, err
	}
	p.Head = strings.TrimSpace(string(head))
	selected := map[string]bool{}
	for _, ref := range refs {
		data, err := git(ctx, p.Root, nil, nil, "rev-parse", "--verify", "--end-of-options", ref+"^{commit}")
		if err != nil {
			return p, err
		}
		id := strings.TrimSpace(string(data))
		if selected[id] {
			return p, fmt.Errorf("duplicate feature commit")
		}
		parents, err := git(ctx, p.Root, nil, nil, "rev-list", "--parents", "-n", "1", id)
		if err != nil || len(strings.Fields(string(parents))) != 2 {
			return p, fmt.Errorf("root and merge commits require manual review")
		}
		if _, err := git(ctx, p.Root, nil, nil, "merge-base", "--is-ancestor", id, p.Head); err != nil {
			return p, fmt.Errorf("commit is not reachable from HEAD")
		}
		selected[id] = true
	}
	ordered, err := git(ctx, p.Root, nil, nil, "rev-list", "--topo-order", p.Head)
	if err != nil {
		return p, err
	}
	for _, id := range strings.Fields(string(ordered)) {
		if selected[id] {
			p.Commits = append(p.Commits, id)
		}
	}
	dir, err := os.MkdirTemp("", "atlas-undo-index-")
	if err != nil {
		return p, err
	}
	defer os.RemoveAll(dir)
	env := []string{"GIT_INDEX_FILE=" + filepath.Join(dir, "index")}
	if _, err := git(ctx, p.Root, env, nil, "read-tree", p.Head); err != nil {
		return p, err
	}
	paths := map[string]bool{}
	for _, id := range p.Commits {
		changed, err := git(ctx, p.Root, nil, nil, "diff-tree", "--no-commit-id", "--name-only", "--no-renames", "-r", "-z", id)
		if err != nil {
			return p, err
		}
		for _, path := range strings.Split(string(changed), "\x00") {
			if path != "" {
				paths[path] = true
			}
		}
		patch, err := git(ctx, p.Root, nil, nil, "diff", "--binary", "--no-ext-diff", "--no-textconv", "--no-renames", id+"^", id, "--")
		if err != nil {
			return p, err
		}
		if _, err := git(ctx, p.Root, env, patch, "apply", "--cached", "--reverse", "--whitespace=nowarn", "-"); err != nil {
			return p, fmt.Errorf("later edits overlap selected feature; resolve manually: %w", err)
		}
	}
	if len(paths) > 256 {
		return p, fmt.Errorf("at most 256 affected paths")
	}
	for path := range paths {
		p.Paths = append(p.Paths, path)
	}
	slices.Sort(p.Paths)
	patch, err := git(ctx, p.Root, env, nil, "diff", "--cached", "--binary", "--no-ext-diff", "--no-textconv", "--no-renames", p.Head, "--")
	if err != nil {
		return p, err
	}
	if len(patch) == 0 {
		return p, fmt.Errorf("selected commits have no net change to undo")
	}
	p.Patch = string(patch)
	for _, path := range p.Paths {
		p.Before[path], err = fileIdentity(p.Root, path)
		if err != nil {
			return p, err
		}
	}
	if _, err := git(ctx, p.Root, nil, patch, "apply", "--check", "--whitespace=nowarn", "-"); err != nil {
		return p, fmt.Errorf("working changes conflict with rollback: %w", err)
	}
	oldest := p.Commits[len(p.Commits)-1]
	args := append([]string{"log", "--format=%H", oldest + ".." + p.Head, "--"}, p.Paths...)
	later, err := git(ctx, p.Root, nil, nil, args...)
	if err != nil {
		return p, err
	}
	for _, id := range strings.Fields(string(later)) {
		if !selected[id] {
			p.LaterChanges = append(p.LaterChanges, id)
		}
	}
	p.ID = p.Fingerprint()
	return p, nil
}

// ApplyUndo checks the exact reviewed plan and file identities immediately
// before Git's all-or-nothing patch application. The index is not modified.
func ApplyUndo(ctx context.Context, p UndoPlan, expected string, acknowledge bool) error {
	if expected == "" || expected != p.ID || p.Fingerprint() != p.ID {
		return fmt.Errorf("rollback plan fingerprint mismatch")
	}
	if len(p.LaterChanges) > 0 && !acknowledge {
		return fmt.Errorf("review possible dependents and acknowledge later changes before applying")
	}
	lockPath, err := git(ctx, p.Root, nil, nil, "rev-parse", "--git-path", "atlas-undo.lock")
	if err != nil {
		return err
	}
	path := strings.TrimSpace(string(lockPath))
	if !filepath.IsAbs(path) {
		path = filepath.Join(p.Root, path)
	}
	lockCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	release, err := lock.File(lockCtx, path)
	if err != nil {
		return err
	}
	defer release()
	head, err := git(ctx, p.Root, nil, nil, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(head)) != p.Head {
		return fmt.Errorf("HEAD changed; prepare a new rollback plan")
	}
	for _, path := range p.Paths {
		now, err := fileIdentity(p.Root, path)
		if err != nil {
			return err
		}
		if now != p.Before[path] {
			return fmt.Errorf("file changed since preview: %s", path)
		}
	}
	if _, err := git(ctx, p.Root, nil, []byte(p.Patch), "apply", "--check", "--whitespace=nowarn", "-"); err != nil {
		return err
	}
	_, err = git(ctx, p.Root, nil, []byte(p.Patch), "apply", "--whitespace=nowarn", "-")
	return err
}
