package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"

	atlaspolicy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/policy"
)

// autoSandbox, when set, lets a command the shell policy would ask about run
// without a prompt if it is inside the execution sandbox and every finding is
// about damage the sandbox contains (files outside the project, privilege).
var autoSandbox atomic.Bool

// SetAutoSandbox turns auto_sandbox mode on or off.
func SetAutoSandbox(on bool) { autoSandbox.Store(on) }

// sandboxContained are the policy rules whose harm the sandbox prevents:
// they act on the user's home or the host, not on the mounted project.
var sandboxContained = map[string]bool{
	"shell-startup-write":  true,
	"privilege-escalation": true,
	"world-writable":       false, // The project is mounted, so this still reaches it.
}

// watchedPatterns are project files whose silent change deserves a note: they
// run code later (git hooks, CI, shell and package configuration) or hold
// credentials.
var watchedPatterns = []string{
	".git/hooks/*", ".github/workflows/*", ".gitlab-ci.yml", ".env", ".env.*", ".npmrc", ".pypirc",
	".envrc", ".bashrc", ".zshrc", ".profile", ".ssh/*",
}

// snapshotWatched records the size and modification time of every watched
// file under root.
func snapshotWatched(root string) map[string]string {
	out := map[string]string{}
	for _, pattern := range watchedPatterns {
		matches, _ := filepath.Glob(filepath.Join(root, filepath.FromSlash(pattern)))
		for _, m := range matches {
			info, err := os.Lstat(m)
			if err != nil || info.IsDir() {
				continue
			}
			rel, _ := filepath.Rel(root, m)
			out[filepath.ToSlash(rel)] = fmt.Sprintf("%d:%d", info.Size(), info.ModTime().UnixNano())
		}
	}
	return out
}

// watchedChanges lists the watched files created, changed or removed between
// two snapshots.
func watchedChanges(before, after map[string]string) []string {
	var changed []string
	for path, sig := range after {
		if old, ok := before[path]; !ok {
			changed = append(changed, path+" (created)")
		} else if old != sig {
			changed = append(changed, path+" (modified)")
		}
	}
	for path := range before {
		if _, ok := after[path]; !ok {
			changed = append(changed, path+" (removed)")
		}
	}
	sort.Strings(changed)
	return changed
}

// monitorNote is the text appended to a result when a command changed
// watched files.
func monitorNote(changed []string) string {
	if len(changed) == 0 {
		return ""
	}
	return "\n\n[Policy monitor] This command changed files that run code later or hold credentials: " + strings.Join(changed, ", ") +
		". If that was not the intent, undo it and tell the user."
}

// verdictContainedBySandbox reports whether every enforced finding that made
// the policy ask is one the sandbox contains.
func verdictContainedBySandbox(v atlaspolicy.Verdict) bool {
	found := false
	for _, f := range v.Findings {
		if !f.Enforced || f.Action != atlaspolicy.Ask {
			continue
		}
		if !sandboxContained[f.RuleID] {
			return false
		}
		found = true
	}
	return found
}
