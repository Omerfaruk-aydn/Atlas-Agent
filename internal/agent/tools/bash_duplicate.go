package tools

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/shell"
)

// runningDuplicate returns a note when the same command is still running as
// a background job in the same folder, and the command is not started again.
// A second dev server or watcher only takes another port ("Port 5173 is in
// use, trying another one...") and leaves two processes to clean up, while
// the agent usually meant to look at the one already running.
func runningDuplicate(command, workingDir string) string {
	key := normalizeJobCommand(command)
	if key == "" {
		return ""
	}
	for _, job := range shell.GetBackgroundShellManager().ListInfo() {
		if job.Done || normalizeJobCommand(job.Command) != key || !sameJobDir(job.WorkingDir, workingDir) {
			continue
		}
		return fmt.Sprintf("Not started: the same command is already running as background job %s (started %s ago in %s); "+
			"a different port does not make it a different server. Dev servers and watchers pick up source changes on their own, "+
			"so keep using that one: job_output shows its address. Stop it with job_kill first only if it is broken.",
			job.ID, time.Since(job.StartedAt).Round(time.Second), filepath.ToSlash(job.WorkingDir))
	}
	return ""
}

// portFlagPattern matches the ways a port is picked on a command line. A
// dev server started again on another port is still the same server, and
// moving to the next free port is exactly how the copies pile up.
var portFlagPattern = regexp.MustCompile(`(?i)(^|\s)(--port[= ]\S+|--strict-?port\b|PORT=\S+)`)

func normalizeJobCommand(command string) string {
	return strings.Join(strings.Fields(portFlagPattern.ReplaceAllString(command, " ")), " ")
}

func sameJobDir(a, b string) bool {
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}
