package agent

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// changeEvidenceMaxBytes bounds the change shown to the reviewer in its
	// first prompt. Larger changes are cut; the reviewer still has git_diff.
	changeEvidenceMaxBytes = 24 << 10
	// changeEvidenceFileBytes bounds one file's share.
	changeEvidenceFileBytes = 8 << 10
	// changeEvidenceTimeout bounds the git calls that gather it.
	changeEvidenceTimeout = 5 * time.Second
)

// changeEvidence returns the diff of the files the agent changed, ready to
// append to the reviewer's request. Handing it over up front spares the
// reviewer the round trips it would otherwise spend on git_diff, git_status
// and view before it can judge anything. Files git does not track yet are
// shown whole. The reviewer keeps its read-only tools for the code around
// the change. An empty result means nothing could be gathered.
func changeEvidence(ctx context.Context, root string, files []string) string {
	if root == "" || len(files) == 0 {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, changeEvidenceTimeout)
	defer cancel()

	var sb strings.Builder
	truncated := false
	for _, file := range files {
		rel, ok := evidenceRelPath(root, file)
		if !ok {
			continue
		}
		section := fileEvidence(ctx, root, rel)
		if section == "" {
			continue
		}
		if sb.Len()+len(section) > changeEvidenceMaxBytes {
			truncated = true
			break
		}
		sb.WriteString(section)
	}
	if sb.Len() == 0 {
		return ""
	}
	out := "\n\nThe change itself, so you can start judging without re-reading it (read further only for the code around it):\n" + sb.String()
	if truncated {
		out += "(Further changed files are not shown here; use git_diff or view for them.)\n"
	}
	return out
}

// evidenceRelPath returns file relative to root, and false for a path that
// lies outside root.
func evidenceRelPath(root, file string) (string, bool) {
	if !filepath.IsAbs(file) {
		file = filepath.Join(root, file)
	}
	rel, err := filepath.Rel(root, file)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

// fileEvidence shows one file: its diff against HEAD when git tracks it,
// otherwise its content as a new file.
func fileEvidence(ctx context.Context, root, rel string) string {
	tracked := exec.CommandContext(ctx, "git", "-C", root, "ls-files", "--error-unmatch", "--", rel).Run() == nil
	if tracked {
		out, err := exec.CommandContext(ctx, "git", "-C", root, "diff", "HEAD", "--no-color", "--", rel).Output()
		if err == nil {
			if len(out) == 0 {
				return ""
			}
			return clipEvidence(fmt.Sprintf("--- %s (changed)\n%s", rel, out))
		}
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil || !utf8.Valid(data) || strings.IndexByte(string(data), 0) >= 0 {
		return ""
	}
	return clipEvidence(fmt.Sprintf("--- %s (new file)\n%s", rel, data))
}

// clipEvidence keeps one file's share within its budget and ends it with a
// newline so sections stay separate.
func clipEvidence(s string) string {
	if len(s) > changeEvidenceFileBytes {
		cut := changeEvidenceFileBytes
		for cut > 0 && !utf8.RuneStart(s[cut]) {
			cut--
		}
		s = s[:cut] + "\n... (cut; use view for the rest)"
	}
	if !strings.HasSuffix(s, "\n") {
		s += "\n"
	}
	return s
}
