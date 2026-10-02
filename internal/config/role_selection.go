package config

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/evaluation"
)

type RoleModelSelection struct {
	ResultsFile string                     `json:"results_file"`
	Policy      evaluation.SelectionPolicy `json:"policy"`
}

// ResolveMeasuredRole selects only from explicit candidates backed by live evidence.
// An enabled but invalid policy returns an error instead of hiding missing evidence.
func (c *ConfigStore) ResolveMeasuredRole(role string) (SelectedModel, bool, error) {
	cfg := c.Config()
	role = StripRoleReference(role)
	if cfg.Options == nil {
		return SelectedModel{}, false, nil
	}
	selection, ok := cfg.Options.RoleModelSelection[role]
	if !ok {
		return SelectedModel{}, false, nil
	}
	if selection.Policy.Role != role || selection.ResultsFile == "" {
		return SelectedModel{}, true, fmt.Errorf("role selection policy must match role %s and name results_file", role)
	}
	p := selection.ResultsFile
	if !filepath.IsAbs(p) {
		p = filepath.Join(c.WorkingDir(), p)
	}
	var records []evaluation.LiveRecord
	if err := evaluation.ReadJSONFile(p, &records); err != nil {
		return SelectedModel{}, true, err
	}
	recommendation, err := evaluation.Recommend(selection.Policy, records, time.Now())
	if err != nil {
		return SelectedModel{}, true, err
	}
	provider, model, _ := strings.Cut(recommendation.Model, "/")
	if cfg.Providers == nil || !cfg.IsModelAvailable(provider, model) {
		return SelectedModel{}, true, fmt.Errorf("measured model %s is unavailable in the configured catalog", recommendation.Model)
	}
	base, _ := cfg.ResolveRole(role)
	if base.Provider != provider || base.Model != model {
		return SelectedModel{Provider: provider, Model: model}, true, nil
	}
	base.Provider, base.Model = provider, model
	return base, true, nil
}
