package engineering

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

type boundedOutput struct {
	data     []byte
	exceeded bool
}

func (b *boundedOutput) Write(p []byte) (int, error) {
	n := len(p)
	if len(b.data)+n > maxStateBytes {
		b.exceeded = true
		p = p[:max(0, maxStateBytes-len(b.data))]
	}
	b.data = append(b.data, p...)
	return n, nil
}

func gitCommand(ctx context.Context, root string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-c", "core.longpaths=true", "-C", root}, args...)...)
	for _, env := range os.Environ() {
		if strings.HasPrefix(env, "GIT_DIR=") || strings.HasPrefix(env, "GIT_WORK_TREE=") || strings.HasPrefix(env, "GIT_INDEX_FILE=") || strings.HasPrefix(env, "GIT_COMMON_DIR=") {
			continue
		}
		cmd.Env = append(cmd.Env, env)
	}
	return cmd
}

func Git(ctx context.Context, root string, args ...string) (string, error) {
	cmd := gitCommand(ctx, root, args...)
	var output boundedOutput
	var stderr boundedOutput
	cmd.Stdout = &output
	cmd.Stderr = &stderr
	err := cmd.Run()
	if output.exceeded || stderr.exceeded {
		return "", errors.New("git output exceeds 1 MiB")
	}
	if err != nil {
		return string(output.data), fmt.Errorf("git %s: %w: %s", args[0], err, string(stderr.data))
	}
	return string(output.data), nil
}

func (s *Store) workspacePath(session, id string) string {
	return filepath.Join(s.dir, "w", Hash(session)[:16], id)
}

func (s *Store) CreateWorkspace(ctx context.Context, root, session, task string) (Workspace, error) {
	base, err := Git(ctx, root, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return Workspace{}, err
	}
	w := Workspace{ID: strings.ReplaceAll(uuid.NewString(), "-", ""), Base: strings.TrimSpace(base), TaskID: task}
	w.CommonDir, err = gitCommonDir(ctx, root)
	if err != nil {
		return w, err
	}
	w.Path = s.workspacePath(session, w.ID)
	if err := os.MkdirAll(filepath.Dir(w.Path), 0o700); err != nil {
		return w, err
	}
	// Reserve before invoking Git: interrupted creation remains inspectable.
	if err := s.Update(ctx, session, func(st *State) error { st.Workspaces = append(st.Workspaces, w); return nil }); err != nil {
		return w, err
	}
	_, err = Git(ctx, root, "worktree", "add", "--detach", w.Path, w.Base)
	return w, err
}

func (s *Store) Workspace(ctx context.Context, session, id string) (Workspace, error) {
	if _, err := uuid.Parse(id); err != nil {
		return Workspace{}, errors.New("invalid managed workspace ID")
	}
	st, err := s.Read(ctx, session)
	if err != nil {
		return Workspace{}, err
	}
	for _, w := range st.Workspaces {
		if w.ID == id {
			if !strings.EqualFold(filepath.Clean(w.Path), filepath.Clean(s.workspacePath(session, id))) {
				return w, errors.New("workspace registry path escapes managed directory")
			}
			info, err := os.Lstat(w.Path)
			if err != nil {
				return w, err
			}
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return w, errors.New("workspace path is not a real directory")
			}
			managedRoot, err := filepath.EvalSymlinks(filepath.Join(s.dir, "w"))
			if err != nil {
				return w, err
			}
			resolved, err := filepath.EvalSymlinks(w.Path)
			if err != nil {
				return w, err
			}
			rel, err := filepath.Rel(managedRoot, resolved)
			if err != nil || !strings.EqualFold(rel, filepath.Join(Hash(session)[:16], id)) {
				return w, errors.New("workspace resolves outside its managed directory")
			}
			actual, err := gitCommonDir(ctx, w.Path)
			if err != nil {
				return w, err
			}
			if !sameDirectory(actual, w.CommonDir) {
				return w, errors.New("workspace belongs to a different Git repository")
			}
			return w, nil
		}
	}
	return Workspace{}, errors.New("managed workspace not found")
}

func (s *Store) WorkspacePatch(ctx context.Context, session, id string) (Workspace, string, error) {
	w, err := s.Workspace(ctx, session, id)
	if err != nil {
		return w, "", err
	}
	untracked, err := Git(ctx, w.Path, "ls-files", "--others", "--exclude-standard")
	if err != nil {
		return w, "", err
	}
	if strings.TrimSpace(untracked) != "" {
		return w, "", errors.New("workspace contains untracked files; review and stage intended new files before exporting a complete patch")
	}
	patch, err := Git(ctx, w.Path, "diff", "--binary", w.Base, "--")
	return w, patch, err
}

func (s *Store) ApplyWorkspace(ctx context.Context, root, session, id string) error {
	w, patch, err := s.WorkspacePatch(ctx, session, id)
	if err != nil {
		return err
	}
	common, err := gitCommonDir(ctx, root)
	if err != nil {
		return err
	}
	if !sameDirectory(common, w.CommonDir) {
		return errors.New("integration target belongs to a different Git repository")
	}
	if patch == "" {
		return errors.New("workspace has no changes")
	}
	for _, args := range [][]string{{"apply", "--check", "--whitespace=nowarn", "-"}, {"apply", "--whitespace=nowarn", "-"}} {
		cmd := gitCommand(ctx, root, args...)
		cmd.Stdin = strings.NewReader(patch)
		var out boundedOutput
		cmd.Stdout = &out
		cmd.Stderr = &out
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("workspace integration failed: %w: %s", err, out.data)
		}
	}
	return s.Update(ctx, session, func(st *State) error {
		for i := range st.Workspaces {
			if st.Workspaces[i].ID == id {
				st.Workspaces[i].AppliedPatchHash = Hash(patch)
				return nil
			}
		}
		return errors.New("applied workspace registry record disappeared")
	})
}

func gitCommonDir(ctx context.Context, root string) (string, error) {
	value, err := Git(ctx, root, "rev-parse", "--git-common-dir")
	if err != nil {
		return "", err
	}
	value = strings.TrimSpace(value)
	if !filepath.IsAbs(value) {
		value = filepath.Join(root, value)
	}
	return filepath.EvalSymlinks(value)
}

func sameDirectory(a, b string) bool {
	x, err := os.Stat(a)
	if err != nil {
		return false
	}
	y, err := os.Stat(b)
	return err == nil && os.SameFile(x, y)
}

func (s *Store) RemoveWorkspace(ctx context.Context, root, session, id string) error {
	w, err := s.Workspace(ctx, session, id)
	if err != nil {
		return err
	}
	status, err := Git(ctx, w.Path, "status", "--porcelain")
	if err != nil {
		return err
	}
	if strings.TrimSpace(status) != "" {
		return errors.New("dirty workspace is preserved; commit or export its changes before removal")
	}
	head, err := Git(ctx, w.Path, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	if strings.TrimSpace(head) != w.Base {
		return errors.New("workspace contains commits beyond its base; preserve them before removal")
	}
	if _, err := Git(ctx, root, "worktree", "remove", w.Path); err != nil {
		return err
	}
	return s.Update(ctx, session, func(st *State) error {
		for i, w := range st.Workspaces {
			if w.ID == id {
				st.Workspaces = append(st.Workspaces[:i], st.Workspaces[i+1:]...)
				break
			}
		}
		return nil
	})
}
