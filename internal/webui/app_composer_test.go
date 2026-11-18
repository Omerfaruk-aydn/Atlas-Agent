package webui

import (
	"net/http"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/stretchr/testify/require"
)

func TestComposerEndpointsRejectInvalidInputs(t *testing.T) {
	h := newHarness(t)
	b := h.signIn()
	ws := b.open(h.project)
	status, body := b.raw(http.MethodPost, wsPath(ws.ID, "/speech"), map[string]string{"not": "wav"})
	require.Equal(t, http.StatusUnprocessableEntity, status)
	require.Contains(t, body, "WAV")
	status, _ = b.raw(http.MethodPost, wsPath(ws.ID, "/speech?language=../../invalid"), nil)
	require.Equal(t, http.StatusBadRequest, status)
	status, _ = b.raw(http.MethodPost, wsPath(ws.ID, "/mcp/missing"), map[string]bool{"enabled": true})
	require.Equal(t, http.StatusNotFound, status)
	status, _ = b.raw(http.MethodPost, wsPath(ws.ID, "/mcp/missing"), map[string]string{})
	require.Equal(t, http.StatusBadRequest, status)
	status, _ = b.raw(http.MethodPost, wsPath(ws.ID, "/mcp/missing/auth"), nil)
	require.Equal(t, http.StatusBadRequest, status)
}

func TestMCPTogglePreservesConfiguredServerAndDottedName(t *testing.T) {
	h := newHarness(t)
	b := h.signIn()
	ws := b.open(h.project)
	server := config.MCPConfig{Type: config.MCPStdio, Command: "atlas-test-mcp-not-installed", Disabled: true}
	require.NoError(t, h.upstream.SetConfigField(t.Context(), ws.ID, config.ScopeWorkspace, "mcp.docs\\.example", server))
	require.NoError(t, h.upstream.SetConfigField(t.Context(), ws.ID, config.ScopeWorkspace, "mcp.other", server))
	status, body := b.raw(http.MethodPost, wsPath(ws.ID, "/mcp/docs.example"), map[string]bool{"enabled": false})
	require.Equal(t, http.StatusOK, status, body)
	cfg, err := h.upstream.GetConfig(t.Context(), ws.ID)
	require.NoError(t, err)
	require.Len(t, cfg.MCP, 2)
	require.Equal(t, server, cfg.MCP["docs.example"])
	require.Equal(t, server, cfg.MCP["other"])
}
