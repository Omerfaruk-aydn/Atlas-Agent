package prompt

import (
	"context"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/factstore"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/projectmodel"
)

// projectModelRefreshTimeout bounds the refresh done while the prompt is
// built. A project too large to refresh in this time keeps the snapshot it
// already has; the project_model tool refreshes without the limit.
const projectModelRefreshTimeout = 4 * time.Second

// loadProjectModel returns the compact project model summary for the system
// prompt, or "" when there is none worth sending.
//
// The block is only ever present when a model has been built on purpose
// (`project model build`, or the project_model tool), and only when its
// summary fits the configured size. It is a pure function of the stored
// model, so it is byte-identical on every turn of a session and for as long
// as the project is unchanged: it never adds per-turn data to the cached
// prefix. Like memory it is read once, when the prompt is built.
func loadProjectModel(ctx context.Context, store *config.ConfigStore) string {
	opts := store.Config().Options
	if !opts.ProjectModel.InjectEnabled() {
		return ""
	}
	dataDir := opts.DataDirectory
	if !filepath.IsAbs(dataDir) {
		dataDir = filepath.Join(store.WorkingDir(), dataDir)
	}
	model, err := projectmodel.Load(dataDir)
	if err != nil {
		slog.Warn("Could not read the project model; continuing without it", "error", err)
		return ""
	}
	if model == nil {
		return ""
	}
	if opts.ProjectModel.AutoRefreshEnabled() {
		svc := projectmodel.NewService(store.WorkingDir(), dataDir, nil, factstore.New(filepath.Join(dataDir, "facts.jsonl")))
		refreshCtx, cancel := context.WithTimeout(ctx, projectModelRefreshTimeout)
		fresh, _, err := svc.Refresh(refreshCtx, false)
		cancel()
		if err != nil {
			slog.Debug("Project model refresh skipped; using the stored snapshot", "error", err)
		} else {
			model = fresh
		}
	}
	return model.Summary(opts.ProjectModel.SummaryBytes())
}
