package engineering

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/subagents"
)

type QualityRun struct {
	ExecutionID string             `json:"execution_id,omitempty"`
	Agent       string             `json:"agent"`
	Handoff     *subagents.Handoff `json:"handoff,omitempty"`
	Error       string             `json:"error,omitempty"`
}

type RoleExecution struct {
	SessionIDs        []string           `json:"session_ids,omitempty"`
	ExecutionID       string             `json:"execution_id,omitempty"`
	MachineChecks     []Check            `json:"machine_checks,omitempty"`
	TaskID            string             `json:"task_id"`
	Agent             string             `json:"agent"`
	TaskFingerprint   string             `json:"task_fingerprint"`
	WorkspaceID       string             `json:"workspace_id,omitempty"`
	Root              string             `json:"root"`
	Handoff           *subagents.Handoff `json:"handoff,omitempty"`
	Error             string             `json:"error,omitempty"`
	RequireReview     bool               `json:"require_review"`
	Reviews           []QualityRun       `json:"reviews,omitempty"`
	SourceFingerprint string             `json:"source_fingerprint,omitempty"`
	Passed            bool               `json:"passed"`
}

// SourceFingerprint covers tracked and non-ignored files, including deletions.
// Incomplete snapshots fail closed instead of certifying an unseen source tree.
func SourceFingerprint(ctx context.Context, root string, excluded ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", root, "ls-files", "-z", "--cached", "--others", "--exclude-standard", "--exclude=.atlas-env/", "--exclude=.venv/", "--exclude=.atlas/interactions/")
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "GIT_") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	var output limitedSnapshot
	cmd.Stdout = &output
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("snapshot git files: %w", err)
	}
	if output.exceeded {
		return "", fmt.Errorf("snapshot file list exceeds limit")
	}
	paths := strings.Split(strings.TrimSuffix(string(output.data), "\x00"), "\x00")
	if len(paths) > 10000 {
		return "", fmt.Errorf("snapshot exceeds 10000 files")
	}
	sort.Strings(paths)
	h := sha256.New()
	var total int64
	seen := map[string]bool{}
	for _, p := range paths {
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		clean := filepath.Clean(p)
		if filepath.IsAbs(p) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf("invalid snapshot path")
		}
		if err := ctx.Err(); err != nil {
			return "", err
		}
		full := filepath.Join(root, p)
		skip := false
		for _, dir := range excluded {
			rel, err := filepath.Rel(dir, full)
			if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel) {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		info, err := os.Lstat(full)
		fmt.Fprintf(h, "%d:%s\x00", len(p), p)
		if os.IsNotExist(err) {
			h.Write([]byte("deleted\x00"))
			continue
		}
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(full)
			if err != nil {
				return "", err
			}
			fmt.Fprintf(h, "link:%s\x00", target)
			continue
		}
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("snapshot cannot inspect %s", p)
		}
		total += info.Size()
		if total > 64*1024*1024 {
			return "", fmt.Errorf("snapshot exceeds 64 MiB")
		}
		fmt.Fprintf(h, "mode:%d:size:%d\x00", info.Mode().Perm(), info.Size())
		f, err := os.Open(full)
		if err != nil {
			return "", err
		}
		n, readErr := io.Copy(h, io.LimitReader(f, info.Size()+1))
		f.Close()
		if readErr != nil {
			return "", readErr
		}
		if n != info.Size() {
			return "", fmt.Errorf("source changed during snapshot")
		}
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

type limitedSnapshot struct {
	data     []byte
	exceeded bool
}

func (b *limitedSnapshot) Write(p []byte) (int, error) {
	n := len(p)
	if len(b.data)+n > 1024*1024 {
		b.exceeded = true
		p = p[:max(0, 1024*1024-len(b.data))]
	}
	b.data = append(b.data, p...)
	return n, nil
}
