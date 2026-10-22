package agent

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/workplan"
)

func snapshotProject(ctx context.Context, root string) (map[string]fileBaseline, error) {
	paths := []string{}
	var data []byte
	gitErr := error(fs.ErrNotExist)
	if hasGitAncestor(root) {
		cmd := exec.CommandContext(ctx, "git", "ls-files", "-z", "--cached", "--others", "--exclude-standard")
		cmd.Dir = root
		data, gitErr = cmd.Output()
	}
	if gitErr == nil {
		for _, p := range strings.Split(string(data), "\x00") {
			if p != "" {
				paths = append(paths, p)
			}
		}
	} else {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if d.IsDir() {
				if path != root && slices.Contains([]string{".git", ".atlas", "node_modules", "vendor", ".venv", "__pycache__"}, d.Name()) {
					return filepath.SkipDir
				}
				return nil
			}
			p, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			paths = append(paths, p)
			if len(paths) > 20000 {
				return fmt.Errorf("project snapshot file bound exceeded")
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	out := map[string]fileBaseline{}
	var bytes int
	for _, p := range paths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		p = filepath.ToSlash(p)
		if workplan.IsAtlasPath(p) || strings.HasPrefix(p, ".git/") {
			continue
		}
		abs := filepath.Join(root, filepath.FromSlash(p))
		info, err := os.Lstat(abs)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() {
			continue
		}
		b := readBaseline(abs)
		if b.tooLarge {
			return nil, fmt.Errorf("source snapshot exceeds per-file read bound")
		}
		bytes += len(b.content)
		if bytes > 64<<20 || len(out) >= 20000 {
			return nil, fmt.Errorf("project snapshot memory bound exceeded")
		}
		out[abs] = b
	}
	return out, nil
}

func hasGitAncestor(root string) bool {
	for dir := filepath.Clean(root); ; dir = filepath.Dir(dir) {
		if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
			return true
		}
		if filepath.Dir(dir) == dir {
			return false
		}
	}
}

// beginProjectEvidence captures the current dirty tree, not HEAD, once per
// request. No pre-existing edit is attributed to this request.
func (a *sessionAgent) beginProjectEvidence(ctx context.Context, sessionID string) {
	if !a.completionGateActive() || a.workingDir == "" {
		return
	}
	l := a.evaluatorLedgerFor(sessionID)
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.projectBefore != nil {
		return
	}
	data, err := snapshotProject(ctx, a.ledgerRoot(l))
	if err != nil {
		l.projectError = err.Error()
		return
	}
	l.projectBefore = data
	l.projectLatest = data
}

func (a *sessionAgent) syncProjectEvidence(ctx context.Context, sessionID string) {
	l := a.evaluatorLedgerFor(sessionID)
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.projectBefore == nil {
		return
	}
	data, err := snapshotProject(ctx, a.ledgerRoot(l))
	if err != nil {
		l.projectError = err.Error()
		return
	}
	changed := map[string]bool{}
	for p, b := range l.projectLatest {
		if now, ok := data[p]; !ok || now.content != b.content {
			changed[p] = true
		}
	}
	for p, b := range data {
		if old, ok := l.projectLatest[p]; !ok || old.content != b.content {
			changed[p] = true
		}
	}
	if len(changed) > 0 {
		l.seq++
		l.lastEdit = l.seq
		l.changed = true
		if l.baselines == nil {
			l.baselines = map[string]fileBaseline{}
		}
		for p := range changed {
			if _, ok := l.baselines[p]; !ok {
				l.baselines[p] = l.projectBefore[p]
			}
			rel, _ := filepath.Rel(a.ledgerRoot(l), p)
			rel = filepath.ToSlash(rel)
			if !slices.Contains(l.files, rel) {
				l.files = append(l.files, rel)
			}
		}
	}
	l.projectLatest = data
}
