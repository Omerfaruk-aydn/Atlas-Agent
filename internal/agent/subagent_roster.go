package agent

import (
	"fmt"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/subagents"
)

// describeConfiguredSubagents renders the names a tool description can
// point an agent at directly. `atlas agent list` is a command for a human
// running the CLI outside the session -- the model calling this tool has
// no shell access to the atlas binary itself, and telling it to run that
// command sends it looking for a way to do so instead of just being told
// the answer it already has configured.
func describeConfiguredSubagents(discovered []*subagents.Subagent) string {
	if len(discovered) == 0 {
		return "\n\nNo subagents are configured right now, so there is nothing to name here yet. " +
			"Do not try to run `atlas agent list` (or any `atlas` command) in a shell to check -- " +
			"this list is authoritative and there is no `atlas` binary reachable from inside this session anyway."
	}
	var b strings.Builder
	b.WriteString("\n\nConfigured subagents you can name here -- this list is authoritative, so do not run " +
		"`atlas agent list` or any other `atlas` shell command to double check it:\n")
	for _, s := range discovered {
		desc := strings.TrimSpace(s.Description)
		if desc == "" {
			fmt.Fprintf(&b, "- %s\n", s.Name)
			continue
		}
		fmt.Fprintf(&b, "- %s: %s\n", s.Name, desc)
	}
	return b.String()
}
