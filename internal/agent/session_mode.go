package agent

import (
	"context"
	"log/slog"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/subagents"
)

// A session mode puts the main session itself into a specialty instead of
// delegating to one: the mode's instructions are folded into the coder
// agent's own system prompt, and when a model role shares the mode's name
// the session switches to that model too. The catalog is the same one the
// agent tool delegates to (see internal/subagents' built-in modes plus
// anything the user has authored), so a mode behaves identically whether
// it is handed work or worn.

// sessionMode returns the mode named by Options.SessionMode, looked up in
// the same catalog named subagents come from. Returns false when no mode
// is selected or when the configured name matches nothing -- a stale name
// left in the config should quietly run the ordinary prompt rather than
// break every session until it is noticed.
func (c *coordinator) sessionMode() (*subagents.Subagent, bool) {
	cfg := c.cfg.Config()
	if cfg == nil || cfg.Options == nil {
		return nil, false
	}
	name := strings.TrimSpace(cfg.Options.SessionMode)
	if name == "" {
		return nil, false
	}

	mode, ok := subagents.Find(subagents.Discover(cfg.Options.SubagentsPaths), name)
	if !ok {
		slog.Warn("Configured session mode does not match any known mode; running the ordinary coder prompt", "mode", name)
		return nil, false
	}
	return mode, true
}

// withSessionMode appends the active mode's instructions to a freshly
// built system prompt, in the same envelope buildSubagentSessionAgent
// uses for a delegated subagent, so the model sees one consistent shape
// either way. Returns systemPrompt unchanged when no mode is active.
func (c *coordinator) withSessionMode(systemPrompt string) string {
	mode, ok := c.sessionMode()
	if !ok || strings.TrimSpace(mode.Instructions) == "" {
		return systemPrompt
	}
	return systemPrompt + "\n\n<mode name=\"" + mode.Name + "\">\n" + mode.Instructions + "\n</mode>" + mode.RolePrompt()
}

// sessionModeModel resolves the model an active session mode should run
// on: the model role sharing its name, exactly as a named subagent
// resolves its own "model" field. Returns false when no mode is active,
// when no role of that name is configured -- a mode with no model
// assigned still contributes its prompt, just on the session's own model
// -- or when that role fails to build, which is logged rather than
// failing the session outright.
func (c *coordinator) sessionModeModel(ctx context.Context) (Model, bool, error) {
	if c.cfg.Overrides().PreserveSelectedModel {
		return Model{}, false, nil
	}
	mode, ok := c.sessionMode()
	if !ok {
		return Model{}, false, nil
	}
	role := mode.Name
	if mode.Model != "" {
		role = mode.Model
	}
	roleCfg, ok := c.cfg.Config().ResolveRole(role)
	measured, enabled, err := c.cfg.ResolveMeasuredRole(role)
	if err != nil {
		return Model{}, false, err
	}
	if enabled {
		roleCfg, ok = measured, true
	}
	if !ok {
		return Model{}, false, nil
	}
	model, err := c.resolveModel(ctx, roleCfg, false)
	if err != nil {
		if enabled {
			return Model{}, false, err
		}
		slog.Warn("Session mode's model role failed to build; staying on the session's own model",
			"mode", mode.Name, "provider", roleCfg.Provider, "model", roleCfg.Model, "error", err)
		return Model{}, false, nil
	}
	return model, true, nil
}
