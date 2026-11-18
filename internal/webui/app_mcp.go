package webui

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/proto"
)

func (g *Gateway) configuredMCP(ctx context.Context, h *hub, name string) error {
	cfg, err := g.workspaceConfig(ctx, h)
	if err != nil {
		return err
	}
	if _, ok := cfg.MCP[name]; !ok {
		return errors.New("MCP server is not configured")
	}
	return nil
}

func (g *Gateway) handleMCPAuthURL(w http.ResponseWriter, r *http.Request, h *hub) {
	ctx, cancel := context.WithTimeout(r.Context(), upstreamTimeout)
	defer cancel()
	name := r.PathValue("name")
	if err := g.configuredMCP(ctx, h, name); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var response proto.MCPAuthResponse
	if err := g.getJSON(ctx, "/workspaces/"+url.PathEscape(h.workspaceID())+"/mcp/auth-url", url.Values{"name": {name}}, &response); err != nil {
		writeUpstreamError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (g *Gateway) handleMCPAuth(w http.ResponseWriter, r *http.Request, h *hub) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	name := r.PathValue("name")
	if err := g.configuredMCP(ctx, h, name); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := g.sendJSON(ctx, http.MethodPost, "/workspaces/"+url.PathEscape(h.workspaceID())+"/mcp/auth", nil, map[string]string{"name": name}, nil); err != nil {
		writeUpstreamError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, struct{}{})
}

// handleMCPEnabled changes only the named, already configured MCP server.
func (g *Gateway) handleMCPEnabled(w http.ResponseWriter, r *http.Request, h *hub) {
	var req struct {
		Enabled *bool `json:"enabled"`
	}
	if !decodeJSON(w, r, maxJSONBody, &req) {
		return
	}
	name := r.PathValue("name")
	if req.Enabled == nil || name == "" || len(name) > 256 {
		writeError(w, http.StatusBadRequest, "a valid MCP name and enabled value are required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*upstreamTimeout)
	defer cancel()
	cfg, err := g.workspaceConfig(ctx, h)
	if err != nil {
		writeUpstreamError(w, err)
		return
	}
	if _, exists := cfg.MCP[name]; !exists {
		writeError(w, http.StatusNotFound, "MCP server is not configured")
		return
	}
	id := url.PathEscape(h.workspaceID())
	var agent proto.AgentInfo
	if err := g.getJSON(ctx, "/workspaces/"+id+"/agent", nil, &agent); err != nil {
		writeUpstreamError(w, err)
		return
	}
	if agent.IsBusy {
		writeError(w, http.StatusConflict, "wait for the current run before changing MCP servers")
		return
	}
	key := strings.NewReplacer("\\", "\\\\", ".", "\\.", ":", "\\:").Replace(name)
	body := proto.ConfigSetRequest{Scope: config.ScopeWorkspace, Key: "mcp." + key + ".disabled", Value: !*req.Enabled}
	if err := g.sendJSON(ctx, http.MethodPost, "/workspaces/"+id+"/config/set", nil, body, nil); err != nil {
		writeUpstreamError(w, err)
		return
	}
	if err := g.sendJSON(ctx, http.MethodPost, "/workspaces/"+id+"/agent/update", nil, nil, nil); err != nil {
		writeUpstreamError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"enabled": *req.Enabled})
}
