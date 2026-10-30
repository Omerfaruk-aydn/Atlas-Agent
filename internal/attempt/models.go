package attempt

import (
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
)

// ResolveModel turns a user-supplied model reference into the form
// "run --model" accepts. A role name ("fast", "large", "@research") becomes
// "provider/model"; anything else passes through unchanged so a plain model
// ID or a "provider/model" pair keeps working. A nil config passes the
// reference through.
func ResolveModel(cfg *config.Config, ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" || cfg == nil {
		return ref
	}
	if selected, ok := cfg.ResolveRole(ref); ok && selected.Model != "" {
		if selected.Provider == "" {
			return selected.Model
		}
		return selected.Provider + "/" + selected.Model
	}
	return ref
}

// ResolveModels resolves every reference in refs.
func ResolveModels(cfg *config.Config, refs []string) []string {
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		out = append(out, ResolveModel(cfg, ref))
	}
	return out
}

// DefaultJudge picks the judge model when the caller names none: a "judge"
// role, else the "evaluator" role, else the configured large model (empty
// string, meaning the default).
func DefaultJudge(cfg *config.Config) string {
	if cfg == nil {
		return ""
	}
	for _, role := range []string{"judge", "evaluator"} {
		if _, ok := cfg.ResolveRole(role); ok {
			return ResolveModel(cfg, role)
		}
	}
	return ""
}
