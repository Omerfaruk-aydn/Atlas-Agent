package engineering

import (
	"slices"
	"unicode/utf8"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/subagents"
)

func shortText(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	text = text[:limit]
	for !utf8.ValidString(text) {
		text = text[:len(text)-1]
	}
	return text + "…"
}

// ProjectExecution exposes bounded reported handoffs separately from observed
// machine checks. It never turns a reported decision into verified completion.
func ProjectExecution(run RoleExecution, details bool) RoleExecution {
	run.Error = shortText(run.Error, 512)
	run.Reviews = slices.Clone(run.Reviews[:min(4, len(run.Reviews))])
	project := func(h *subagents.Handoff) *subagents.Handoff {
		if h == nil || !details {
			return nil
		}
		out := *h
		out.Summary = shortText(out.Summary, 1024)
		out.ChangedFiles = slices.Clone(out.ChangedFiles[:min(16, len(out.ChangedFiles))])
		out.Risks = slices.Clone(out.Risks[:min(4, len(out.Risks))])
		out.Dependencies = slices.Clone(out.Dependencies[:min(8, len(out.Dependencies))])
		out.Checks = slices.Clone(out.Checks[:min(4, len(out.Checks))])
		for i := range out.Checks {
			out.Checks[i].Command = shortText(out.Checks[i].Command, 256)
			out.Checks[i].Evidence = shortText(out.Checks[i].Evidence, 256)
		}
		out.Findings = nil
		return &out
	}
	run.Handoff = project(run.Handoff)
	for i := range run.Reviews {
		run.Reviews[i].Handoff = project(run.Reviews[i].Handoff)
		run.Reviews[i].Error = shortText(run.Reviews[i].Error, 512)
	}
	return run
}
