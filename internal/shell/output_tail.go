package shell

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// BackgroundOutput is a bounded view of actual process output.
type BackgroundOutput struct {
	Stdout    string `json:"stdout"`
	Stderr    string `json:"stderr"`
	Done      bool   `json:"done"`
	Error     string `json:"error,omitempty"`
	Truncated bool   `json:"truncated"`
}

func (sb *syncBuffer) tail(limit int) (string, bool) {
	sb.mu.RLock()
	defer sb.mu.RUnlock()
	data := sb.buf.Bytes()
	truncated := len(data) > limit
	if truncated {
		data = data[len(data)-limit:]
		for len(data) > 0 && !utf8.RuneStart(data[0]) {
			data = data[1:]
		}
	}
	return strings.ToValidUTF8(string(data), "�"), truncated
}

// OutputForRoot excludes jobs belonging to another project, including jobs
// whose working directory reaches outside the project through a symlink.
func (m *BackgroundShellManager) OutputForRoot(root, id string) (BackgroundOutput, error) {
	job, ok := m.Get(id)
	if !ok {
		return BackgroundOutput{}, fmt.Errorf("background job not found")
	}
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		return BackgroundOutput{}, err
	}
	work, err := filepath.EvalSymlinks(job.WorkingDir)
	if err != nil {
		return BackgroundOutput{}, err
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return BackgroundOutput{}, err
	}
	work, err = filepath.Abs(work)
	if err != nil {
		return BackgroundOutput{}, err
	}
	rel, err := filepath.Rel(root, work)
	if err != nil || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return BackgroundOutput{}, fmt.Errorf("background job belongs to another project")
	}
	stdout, a := job.stdout.tail(32 * 1024)
	stderr, b := job.stderr.tail(32 * 1024)
	out := BackgroundOutput{Stdout: stdout, Stderr: stderr, Done: job.IsDone(), Truncated: a || b}
	if out.Done && job.exitErr != nil {
		out.Error = job.exitErr.Error()
	}
	return out, nil
}
