package rehearsal

import (
	"fmt"
	"strings"
)

// Render formats a report as plain text for the terminal and for the
// model. It is the preview the user approves before anything is applied.
func Render(r *Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Rehearsal %s [%s] verdict: %s\n", r.ID, r.Status, r.Verdict)
	if r.Task != "" {
		fmt.Fprintf(&b, "Task: %s\n", r.Task)
	}

	var steps []CommandResult
	for _, c := range r.Commands {
		if c.Phase != "check" {
			steps = append(steps, c)
		}
	}
	if len(steps) > 0 {
		b.WriteString("\nCommands executed in the isolated copy:\n")
		for _, c := range steps {
			fmt.Fprintf(&b, "  [%d] %s\n", c.ExitCode, c.Command)
			if c.ExitCode != 0 && c.Output != "" {
				b.WriteString(indent(c.Output, "      "))
			}
		}
	}

	fmt.Fprintf(&b, "\nFiles changed: %d", len(r.Files))
	if r.IgnoredChanges > 0 {
		fmt.Fprintf(&b, " (+%d git-ignored paths not applied)", r.IgnoredChanges)
	}
	b.WriteString("\n")
	for i, f := range r.Files {
		if i == 60 {
			fmt.Fprintf(&b, "  ... and %d more\n", len(r.Files)-i)
			break
		}
		if f.Binary {
			fmt.Fprintf(&b, "  %-9s %s (binary)\n", f.Status, f.Path)
			continue
		}
		fmt.Fprintf(&b, "  %-9s %s +%d -%d\n", f.Status, f.Path, f.Added, f.Removed)
	}

	if len(r.Checks) > 0 {
		b.WriteString("\nChecks (before -> after):\n")
		for _, c := range r.Checks {
			before := "not run"
			if c.Baseline != nil {
				before = fmt.Sprint(*c.Baseline)
			}
			fmt.Fprintf(&b, "  %-14s %s -> %d  %s\n", c.Verdict, before, c.After, c.Command)
			for _, name := range c.NewlyFailingTests {
				fmt.Fprintf(&b, "      newly failing: %s\n", name)
			}
		}
		for _, c := range r.Commands {
			if c.Phase == "check" && c.ExitCode != 0 && c.Output != "" {
				fmt.Fprintf(&b, "\nOutput of failing check %q:\n%s", c.Command, indent(c.Output, "  "))
			}
		}
	}

	if r.API != nil {
		if r.API.Empty() {
			b.WriteString("\nExported Go API: unchanged\n")
		} else {
			b.WriteString("\nExported Go API changes:\n")
			for _, group := range []struct {
				name  string
				items []string
			}{{"added", r.API.Added}, {"removed", r.API.Removed}, {"changed", r.API.Changed}} {
				for _, item := range group.items {
					fmt.Fprintf(&b, "  %s %s\n", group.name, item)
				}
			}
		}
	}
	for _, w := range r.Warnings {
		fmt.Fprintf(&b, "\nWarning: %s", w)
	}
	if len(r.Warnings) > 0 {
		b.WriteString("\n")
	}

	b.WriteString("\nOnly file changes are applied; side effects outside files (databases, installed packages, remote services) are not, so re-run those commands for real after applying.\n")
	switch r.Status {
	case "previewed":
		fmt.Fprintf(&b, "Apply with action \"apply\" (or `atlas-agent rehearse apply %s`); discard with \"discard\".\n", r.ID)
	}
	return b.String()
}

func indent(s, prefix string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, l := range lines {
		lines[i] = prefix + l
	}
	return strings.Join(lines, "\n") + "\n"
}
