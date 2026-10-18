package agent

import (
	"context"
	"path/filepath"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/harnessmetrics"
)

// harnessDataDir resolves where harness events are written: the configured
// data directory, read on every event so a changed setting applies at once,
// else the project's default .atlas directory.
func harnessDataDir(cfg *config.ConfigStore, workingDir string) func() string {
	return func() string {
		if cfg != nil {
			if c := cfg.Config(); c != nil && c.Options != nil && c.Options.DataDirectory != "" {
				return c.Options.DataDirectory
			}
		}
		if workingDir == "" {
			return ""
		}
		return filepath.Join(workingDir, ".atlas")
	}
}

// recordHarness records one harness event for a session. Sub-agents are
// not recorded: their turns are steps of the parent's work, and counting
// them would double the parent's numbers.
func (a *sessionAgent) recordHarness(kind harnessmetrics.Kind, sessionID string, attrs map[string]string) {
	if a.isSubAgent {
		return
	}
	a.harness.Record(kind, sessionID, attrs)
}

// observeToolSearch records how each tool search ended, so the report shows
// which capability words find nothing and need an alias or hint.
func observeToolSearch(r *harnessmetrics.Recorder, isSubAgent bool) func(ctx context.Context, query string, results, closest int) {
	return func(ctx context.Context, query string, results, closest int) {
		if isSubAgent {
			return
		}
		kind := harnessmetrics.ToolSearchHit
		switch {
		case results > 0:
		case closest > 0:
			kind = harnessmetrics.ToolSearchClosest
		default:
			kind = harnessmetrics.ToolSearchEmpty
		}
		r.Record(kind, tools.GetSessionFromContext(ctx), map[string]string{"query": query})
	}
}
