package agent

import (
	"context"
	"log/slog"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
)

// roleModelFor resolves a model role named after an agent, when one is
// configured. The built-in "large" and "small" names are not roles here,
// and a role that fails to build is logged and ignored so routing can never
// break an agent that worked without it.
func (c *coordinator) roleModelFor(ctx context.Context, name string) (Model, bool) {
	name = strings.TrimSpace(name)
	if name == "" || name == string(config.SelectedModelTypeLarge) || name == string(config.SelectedModelTypeSmall) {
		return Model{}, false
	}
	cfg := c.cfg.Config()
	if cfg.Options == nil {
		return Model{}, false
	}
	selected, ok := cfg.Options.ModelRoles[name]
	if !ok {
		return Model{}, false
	}
	model, err := c.resolveModel(ctx, selected, true)
	if err != nil {
		slog.Warn("Model role named after an agent failed to build; using the default model", "role", name, "error", err)
		return Model{}, false
	}
	return model, true
}
