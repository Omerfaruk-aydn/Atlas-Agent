package evaluation

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/harnessmetrics"
)

// ExportCase turns a failure corpus case into a bench task under suite:
// repo/ holds the project as it was before the request (the case's end
// state with the files the request changed restored), and task.json the
// request as the prompt and the case's checks as the judge. check overrides
// the judge; a case without checks needs one. The task can then be run with
// `atlas eval bench` like any other.
func ExportCase(ctx context.Context, c harnessmetrics.Case, suite string, check []string) (string, error) {
	if !benchIDPattern.MatchString(c.ID) {
		return "", fmt.Errorf("case id %q is not a valid task id", c.ID)
	}
	if c.Commit == "" {
		return "", fmt.Errorf("case %s has no commit: the project was not a git repository when it was saved", c.ID)
	}
	if len(check) == 0 {
		if len(c.Checks) == 0 {
			return "", fmt.Errorf("case %s recorded no checks; pass one with --check", c.ID)
		}
		check = []string{"bash", "-c", strings.Join(c.Checks, " && ")}
	}
	taskDir := filepath.Join(suite, c.ID)
	if _, err := os.Stat(taskDir); err == nil {
		return "", fmt.Errorf("%s already exists", taskDir)
	}
	repo := filepath.Join(taskDir, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		return "", err
	}
	if err := extractCommit(ctx, c.WorkingDir, c.Commit, repo); err != nil {
		_ = os.RemoveAll(taskDir)
		return "", err
	}
	for _, f := range c.Files {
		if f.Unknown {
			continue
		}
		target, err := safeJoin(repo, f.Path)
		if err != nil {
			continue
		}
		if !f.Existed {
			_ = os.Remove(target)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(target, []byte(f.Before), 0o644); err != nil {
			return "", err
		}
	}
	task := BenchTask{ID: c.ID, Prompt: c.Request, Check: check, TimeoutSeconds: 600}
	data, err := json.MarshalIndent(task, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(taskDir, "task.json"), data, 0o644); err != nil {
		return "", err
	}
	return taskDir, nil
}

// extractCommit writes the tree of commit in the repository at root into
// dir, using git archive so the user's checkout is not touched.
func extractCommit(ctx context.Context, root, commit, dir string) error {
	cmd := exec.CommandContext(ctx, "git", "archive", "--format=tar", commit)
	cmd.Dir = root
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git archive %s: %w: %s", commit, err, strings.TrimSpace(stderr.String()))
	}
	reader := tar.NewReader(&out)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		target, err := safeJoin(dir, header.Name)
		if err != nil {
			continue
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if header.Size > benchMaxFileSize*16 {
				continue
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			data, err := io.ReadAll(io.LimitReader(reader, header.Size))
			if err != nil {
				return err
			}
			mode := os.FileMode(0o644)
			if header.Mode&0o111 != 0 {
				mode = 0o755
			}
			if err := os.WriteFile(target, data, mode); err != nil {
				return err
			}
		}
	}
}

// safeJoin joins a slash-separated relative path to root, refusing paths
// that would leave it.
func safeJoin(root, rel string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(rel))
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q leaves the workspace", rel)
	}
	return filepath.Join(root, clean), nil
}
