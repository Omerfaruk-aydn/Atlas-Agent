package agent

import (
	"sort"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
)

// differentProviderRole picks deterministically among configured roles. An
// explicit evaluator role always wins; this helper only chooses the default.
func differentProviderRole(cfg *config.Config, authorProvider string, advisor *Model) (string, bool) {
	if cfg == nil || cfg.Options == nil {
		return "", false
	}
	if advisor != nil && advisor.ModelCfg.Provider != authorProvider {
		if _, ok := cfg.Options.ModelRoles["advisor"]; ok {
			return "advisor", true
		}
	}
	names := make([]string, 0, len(cfg.Options.ModelRoles))
	for name, selected := range cfg.Options.ModelRoles {
		if name != "evaluator" && selected.Provider != "" && selected.Provider != authorProvider {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		return "", false
	}
	return names[0], true
}
