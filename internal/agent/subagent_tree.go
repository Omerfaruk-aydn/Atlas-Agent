package agent

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// maxTreeFiles bounds a snapshot so a huge checkout cannot slow a delegation.
const maxTreeFiles = 50_000

// treeStamps records every tracked and untracked (not ignored) file of a git
// checkout as "size:modified-time", keyed by its path relative to root. It
// returns nil outside a repository or when the tree is too large, in which
// case a run is judged only by the files its edit tools named.
func treeStamps(ctx context.Context, root string) map[string]string {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", root, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "GIT_") || strings.HasPrefix(entry, "GIT_CEILING_DIRECTORIES=") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	names := bytes.Split(bytes.TrimRight(out, "\x00"), []byte{0})
	if len(names) > maxTreeFiles {
		return nil
	}
	stamps := make(map[string]string, len(names))
	for _, raw := range names {
		name := string(raw)
		if name == "" {
			continue
		}
		info, err := os.Stat(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil {
			stamps[name] = "gone"
			continue
		}
		stamps[name] = strconv.FormatInt(info.Size(), 10) + ":" + strconv.FormatInt(info.ModTime().UnixNano(), 10)
	}
	return stamps
}

// changedBetween lists the files added, removed or modified between two
// snapshots, in order, leaving out Atlas's own state under .atlas/. A nil
// snapshot means "unknown", so nothing is reported.
func changedBetween(before, after map[string]string) []string {
	if before == nil || after == nil {
		return nil
	}
	var changed []string
	for name, stamp := range after {
		if strings.HasPrefix(name, ".atlas/") {
			continue
		}
		if prev, ok := before[name]; !ok || prev != stamp {
			changed = append(changed, name)
		}
	}
	for name := range before {
		if _, ok := after[name]; !ok && !strings.HasPrefix(name, ".atlas/") {
			changed = append(changed, name)
		}
	}
	slices.Sort(changed)
	return changed
}
