package webui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/lsp"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/proto"
)

// modelView describes one model the way the composer needs it. Values the
// catalog does not know stay at their zero value and are omitted, so the
// browser can tell "unknown" from "zero".
type modelView struct {
	ID                     string   `json:"id"`
	Name                   string   `json:"name"`
	ContextWindow          int64    `json:"context_window,omitempty"`
	CanReason              bool     `json:"can_reason"`
	ReasoningLevels        []string `json:"reasoning_levels,omitempty"`
	DefaultReasoningEffort string   `json:"default_reasoning_effort,omitempty"`
	SupportsImages         bool     `json:"supports_images"`
	CostPer1MIn            float64  `json:"cost_per_1m_in,omitempty"`
	CostPer1MOut           float64  `json:"cost_per_1m_out,omitempty"`
}

type providerView struct {
	ID     string      `json:"id"`
	Name   string      `json:"name"`
	Models []modelView `json:"models"`
}

type selectedView struct {
	Provider        string `json:"provider"`
	Model           string `json:"model"`
	ReasoningEffort string `json:"reasoning_effort,omitempty"`
	Think           bool   `json:"think,omitempty"`
}

// catalog is the browser's model catalog: the enabled providers and what
// each model role is set to. It is derived from the workspace config but
// carries none of its credentials, URLs or headers.
type catalog struct {
	Providers []providerView            `json:"providers"`
	Selected  map[string]selectedView   `json:"selected"`
	Active    *selectedView             `json:"active,omitempty"`
	Effective map[string]map[string]any `json:"effective,omitempty"`
}

func (g *Gateway) workspaceConfig(ctx context.Context, h *hub) (*config.Config, error) {
	var cfg config.Config
	if err := g.getJSON(ctx, "/workspaces/"+url.PathEscape(h.workspaceID())+"/config", nil, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func buildCatalog(cfg *config.Config, agent *proto.AgentInfo) catalog {
	out := catalog{Selected: map[string]selectedView{}}
	for _, p := range cfg.EnabledProviders() {
		pv := providerView{ID: p.ID, Name: p.Name}
		if pv.Name == "" {
			pv.Name = p.ID
		}
		for _, m := range p.Models {
			name := m.Name
			if name == "" {
				name = m.ID
			}
			pv.Models = append(pv.Models, modelView{
				ID:                     m.ID,
				Name:                   name,
				ContextWindow:          m.ContextWindow,
				CanReason:              m.CanReason,
				ReasoningLevels:        m.ReasoningLevels,
				DefaultReasoningEffort: m.DefaultReasoningEffort,
				SupportsImages:         m.SupportsImages,
				CostPer1MIn:            m.CostPer1MIn,
				CostPer1MOut:           m.CostPer1MOut,
			})
		}
		if len(pv.Models) == 0 {
			continue
		}
		sort.SliceStable(pv.Models, func(i, j int) bool { return strings.ToLower(pv.Models[i].Name) < strings.ToLower(pv.Models[j].Name) })
		out.Providers = append(out.Providers, pv)
	}
	sort.SliceStable(out.Providers, func(i, j int) bool {
		return strings.ToLower(out.Providers[i].Name) < strings.ToLower(out.Providers[j].Name)
	})
	for typ, sel := range cfg.Models {
		out.Selected[string(typ)] = selectedView{Provider: sel.Provider, Model: sel.Model, ReasoningEffort: sel.ReasoningEffort, Think: sel.Think}
	}
	if agent != nil && agent.IsReady {
		out.Active = &selectedView{
			Provider:        agent.ModelCfg.Provider,
			Model:           agent.ModelCfg.Model,
			ReasoningEffort: agent.ModelCfg.ReasoningEffort,
			Think:           agent.ModelCfg.Think,
		}
	}
	return out
}

func (g *Gateway) handleModels(w http.ResponseWriter, r *http.Request, h *hub) {
	ctx, cancel := context.WithTimeout(r.Context(), upstreamTimeout)
	defer cancel()
	cfg, err := g.workspaceConfig(ctx, h)
	if err != nil {
		writeUpstreamError(w, err)
		return
	}
	var agent proto.AgentInfo
	if err := g.getJSON(ctx, "/workspaces/"+url.PathEscape(h.workspaceID())+"/agent", nil, &agent); err != nil {
		writeJSON(w, http.StatusOK, buildCatalog(cfg, nil))
		return
	}
	writeJSON(w, http.StatusOK, buildCatalog(cfg, &agent))
}

// handleSelectModel sets the model for a role through the server's own
// preferred-model path and reloads the agent, then reports the model the
// agent actually runs so the browser can show any difference.
func (g *Gateway) handleSelectModel(w http.ResponseWriter, r *http.Request, h *hub) {
	var req struct {
		Type            string `json:"type"`
		Provider        string `json:"provider"`
		Model           string `json:"model"`
		ReasoningEffort string `json:"reasoning_effort"`
		Think           bool   `json:"think"`
		Scope           string `json:"scope"`
	}
	if !decodeJSON(w, r, maxJSONBody, &req) {
		return
	}
	modelType := config.SelectedModelType(req.Type)
	if modelType != config.SelectedModelTypeLarge && modelType != config.SelectedModelTypeSmall {
		writeError(w, http.StatusBadRequest, "type must be large or small")
		return
	}
	scope, ok := parseScope(req.Scope)
	if !ok {
		writeError(w, http.StatusBadRequest, "scope must be global or workspace")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*upstreamTimeout)
	defer cancel()
	cfg, err := g.workspaceConfig(ctx, h)
	if err != nil {
		writeUpstreamError(w, err)
		return
	}
	model := cfg.GetModel(req.Provider, req.Model)
	if model == nil {
		writeError(w, http.StatusBadRequest, "unknown provider or model")
		return
	}
	if req.ReasoningEffort != "" && (!model.CanReason || (len(model.ReasoningLevels) > 0 && !slices.Contains(model.ReasoningLevels, req.ReasoningEffort))) {
		writeError(w, http.StatusBadRequest, "this model does not support that reasoning effort")
		return
	}
	if req.Think && !model.CanReason {
		writeError(w, http.StatusBadRequest, "this model does not support thinking")
		return
	}
	// Start from the role's current settings so options the browser does
	// not edit (max tokens, temperature, provider options) survive.
	selected := cfg.Models[modelType]
	if selected.Provider != req.Provider || selected.Model != req.Model {
		selected = config.SelectedModel{}
	}
	selected.Provider, selected.Model = req.Provider, req.Model
	selected.ReasoningEffort, selected.Think = req.ReasoningEffort, req.Think

	id := url.PathEscape(h.workspaceID())
	if err := g.sendJSON(ctx, http.MethodPost, "/workspaces/"+id+"/config/model", nil, proto.ConfigModelRequest{Scope: scope, ModelType: modelType, Model: selected}, nil); err != nil {
		writeUpstreamError(w, err)
		return
	}
	if err := g.sendJSON(ctx, http.MethodPost, "/workspaces/"+id+"/agent/update", nil, nil, nil); err != nil {
		writeUpstreamError(w, err)
		return
	}
	g.handleModels(w, r, h)
}

// lspStateName names a language server state for display.
func lspStateName(s lsp.ServerState) string {
	switch s {
	case lsp.StateUnstarted:
		return "unstarted"
	case lsp.StateStarting:
		return "starting"
	case lsp.StateReady:
		return "ready"
	case lsp.StateError:
		return "error"
	case lsp.StateStopped:
		return "stopped"
	case lsp.StateDisabled:
		return "disabled"
	}
	return "unknown"
}

func parseScope(s string) (config.Scope, bool) {
	switch s {
	case "", "global":
		return config.ScopeGlobal, true
	case "workspace":
		return config.ScopeWorkspace, true
	}
	return 0, false
}

// handleSettings reports what the settings dialog shows: modes, MCP
// servers, skills and language servers, as the server sees them.
func (g *Gateway) handleSettings(w http.ResponseWriter, r *http.Request, h *hub) {
	ctx, cancel := context.WithTimeout(r.Context(), upstreamTimeout)
	defer cancel()
	id := url.PathEscape(h.workspaceID())
	cfg, err := g.workspaceConfig(ctx, h)
	if err != nil {
		writeUpstreamError(w, err)
		return
	}

	type modeView struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	modes := []modeView{}
	var subagents []proto.Subagent
	if err := g.getJSON(ctx, "/workspaces/"+id+"/agents", nil, &subagents); err == nil {
		for _, s := range subagents {
			desc := s.Description
			if i := strings.Index(desc, ". "); i > 0 {
				desc = desc[:i+1]
			}
			modes = append(modes, modeView{Name: s.Name, Description: desc})
		}
	}
	sort.Slice(modes, func(i, j int) bool { return modes[i].Name < modes[j].Name })

	var mode proto.PermissionModeRequest
	_ = g.getJSON(ctx, "/workspaces/"+id+"/permissions/mode", nil, &mode)

	type mcpView struct {
		Name      string `json:"name"`
		State     string `json:"state"`
		Error     string `json:"error,omitempty"`
		Tools     int    `json:"tools,omitempty"`
		Prompts   int    `json:"prompts,omitempty"`
		Resources int    `json:"resources,omitempty"`
	}
	mcps := []mcpView{}
	var rawMCP map[string]json.RawMessage
	if err := g.getJSON(ctx, "/workspaces/"+id+"/mcp/states", nil, &rawMCP); err == nil {
		for name, raw := range rawMCP {
			var info struct {
				State         string          `json:"state"`
				Error         json.RawMessage `json:"error"`
				ToolCount     int             `json:"tool_count"`
				PromptCount   int             `json:"prompt_count"`
				ResourceCount int             `json:"resource_count"`
			}
			if json.Unmarshal(raw, &info) != nil {
				continue
			}
			v := mcpView{Name: name, State: info.State, Tools: info.ToolCount, Prompts: info.PromptCount, Resources: info.ResourceCount}
			var msg string
			if json.Unmarshal(info.Error, &msg) == nil {
				v.Error = msg
			}
			mcps = append(mcps, v)
		}
	}
	sort.Slice(mcps, func(i, j int) bool { return mcps[i].Name < mcps[j].Name })

	var ws proto.Workspace
	_ = g.getJSON(ctx, "/workspaces/"+id, nil, &ws)
	skills := ws.Skills
	if skills == nil {
		skills = []proto.SkillState{}
	}

	type lspView struct {
		Name        string `json:"name"`
		State       string `json:"state"`
		Diagnostics int    `json:"diagnostics,omitempty"`
	}
	lsps := []lspView{}
	var rawLSP map[string]proto.LSPClientInfo
	if err := g.getJSON(ctx, "/workspaces/"+id+"/lsps", nil, &rawLSP); err == nil {
		for name, info := range rawLSP {
			lsps = append(lsps, lspView{Name: name, State: lspStateName(info.State), Diagnostics: info.DiagnosticCount})
		}
	}
	sort.Slice(lsps, func(i, j int) bool { return lsps[i].Name < lsps[j].Name })

	sessionMode := ""
	if cfg.Options != nil {
		sessionMode = strings.TrimSpace(cfg.Options.SessionMode)
	}
	autoCompact, autoCompactAt := true, 0
	if cfg.Options != nil {
		autoCompact = !cfg.Options.DisableAutoSummarize
		if at := cfg.Options.AutoSummarizeAt; at > 0 && at < 1 {
			autoCompactAt = int(at*100 + 0.5)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"session_mode":    sessionMode,
		"modes":           modes,
		"permission_mode": mode.Mode,
		"auto_compact":    autoCompact,
		"auto_compact_at": autoCompactAt,
		"mcp":             mcps,
		"skills":          skills,
		"lsp":             lsps,
	})
}

func (g *Gateway) handlePermissionMode(w http.ResponseWriter, r *http.Request, h *hub) {
	var req proto.PermissionModeRequest
	if !decodeJSON(w, r, maxJSONBody, &req) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), upstreamTimeout)
	defer cancel()
	var out proto.PermissionModeRequest
	if err := g.sendJSON(ctx, http.MethodPost, "/workspaces/"+url.PathEscape(h.workspaceID())+"/permissions/mode", nil, req, &out); err != nil {
		writeUpstreamError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// handleCompaction turns automatic summarizing on or off and sets how full
// the context window gets before it runs, like the terminal's /auto-compact
// and /auto-compact-at. Either field may be left out. A percent of 0 goes
// back to the built-in thresholds.
func (g *Gateway) handleCompaction(w http.ResponseWriter, r *http.Request, h *hub) {
	var req struct {
		Enabled *bool    `json:"enabled"`
		Percent *float64 `json:"percent"`
	}
	if !decodeJSON(w, r, maxJSONBody, &req) {
		return
	}
	if req.Enabled == nil && req.Percent == nil {
		writeError(w, http.StatusBadRequest, "enabled or percent is required")
		return
	}
	if req.Percent != nil && *req.Percent != 0 && (*req.Percent < 1 || *req.Percent > 99) {
		writeError(w, http.StatusBadRequest, "percent must be between 1 and 99, or 0 for the built-in thresholds")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), upstreamTimeout)
	defer cancel()
	path := "/workspaces/" + url.PathEscape(h.workspaceID()) + "/config/set"
	if req.Enabled != nil {
		body := proto.ConfigSetRequest{Scope: config.ScopeGlobal, Key: "options.disable_auto_summarize", Value: !*req.Enabled}
		if err := g.sendJSON(ctx, http.MethodPost, path, nil, body, nil); err != nil {
			writeUpstreamError(w, err)
			return
		}
	}
	if req.Percent != nil {
		body := proto.ConfigSetRequest{Scope: config.ScopeGlobal, Key: "options.auto_summarize_at", Value: *req.Percent / 100}
		if err := g.sendJSON(ctx, http.MethodPost, path, nil, body, nil); err != nil {
			writeUpstreamError(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, struct{}{})
}

// handleSessionMode puts the agent into a mode the way the terminal's
// /mode does: it writes options.session_mode to the global config and
// rebuilds the agent around it. It refuses while a run is in flight,
// like the terminal, because the rebuild replaces the running agent's
// prompt.
func (g *Gateway) handleSessionMode(w http.ResponseWriter, r *http.Request, h *hub) {
	var req struct {
		Mode string `json:"mode"`
	}
	if !decodeJSON(w, r, maxJSONBody, &req) {
		return
	}
	mode := strings.TrimSpace(req.Mode)
	ctx, cancel := context.WithTimeout(r.Context(), 2*upstreamTimeout)
	defer cancel()
	id := url.PathEscape(h.workspaceID())
	if mode != "" {
		var subagents []proto.Subagent
		if err := g.getJSON(ctx, "/workspaces/"+id+"/agents", nil, &subagents); err != nil {
			writeUpstreamError(w, err)
			return
		}
		if !slices.ContainsFunc(subagents, func(s proto.Subagent) bool { return s.Name == mode }) {
			writeError(w, http.StatusBadRequest, "unknown mode")
			return
		}
	}
	var agent proto.AgentInfo
	if err := g.getJSON(ctx, "/workspaces/"+id+"/agent", nil, &agent); err == nil && agent.IsBusy {
		writeError(w, http.StatusConflict, "the agent is busy; switch modes when it is idle")
		return
	}
	if err := g.sendJSON(ctx, http.MethodPost, "/workspaces/"+id+"/config/set", nil, proto.ConfigSetRequest{Scope: config.ScopeGlobal, Key: "options.session_mode", Value: mode}, nil); err != nil {
		writeUpstreamError(w, err)
		return
	}
	if err := g.sendJSON(ctx, http.MethodPost, "/workspaces/"+id+"/agent/update", nil, nil, nil); err != nil {
		writeUpstreamError(w, err)
		return
	}
	cfg, err := g.workspaceConfig(ctx, h)
	if err != nil {
		writeUpstreamError(w, err)
		return
	}
	applied := ""
	if cfg.Options != nil {
		applied = strings.TrimSpace(cfg.Options.SessionMode)
	}
	writeJSON(w, http.StatusOK, map[string]any{"session_mode": applied, "scope": "global"})
}
