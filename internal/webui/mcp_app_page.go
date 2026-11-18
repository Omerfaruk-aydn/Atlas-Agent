package webui

import (
	"context"
	"net/http"
	"net/url"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/proto"
)

// mcpAppPolicy is the policy of an MCP app page. It is the page's own, not the
// client's: the client's policy would stop the page's scripts, and the page
// must not be able to make a single request of its own. The sandbox directive
// gives it a throwaway origin even if it is opened outside its frame, and
// frame-ancestors lets only the client frame it.
const mcpAppPolicy = "sandbox allow-scripts; default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; " +
	"img-src data:; font-src data:; frame-ancestors 'self'"

// mcpAppBridge runs inside the page. window.atlas.callTool posts a message to
// the client and resolves with the client's answer; nothing else leaves the
// page.
const mcpAppBridge = `<script>(function(){var n=0,p={};window.addEventListener("message",function(e){var m=e.data;if(!m||m.type!=="atlas-mcp-result"||!p[m.id])return;var q=p[m.id];delete p[m.id];m.error?q.reject(new Error(m.error)):q.resolve(m.result)});window.atlas={callTool:function(name,args){return new Promise(function(resolve,reject){var id=++n;p[id]={resolve:resolve,reject:reject};parent.postMessage({type:"atlas-mcp-call",id:id,tool:String(name),arguments:args||{}},"*")})}}})();</script>`

// mcpAppDocument is the page's HTML with the call bridge in front of it.
func mcpAppDocument(html string) string {
	return "<!doctype html><meta charset=\"utf-8\">" + mcpAppBridge + html
}

// handleMCPAppPage serves an MCP app page as its own document, so that it can
// carry its own policy instead of inheriting the client's.
func (g *Gateway) handleMCPAppPage(w http.ResponseWriter, r *http.Request, h *hub) {
	tool := r.URL.Query().Get("tool")
	if tool == "" || len(tool) > 256 {
		writeError(w, http.StatusBadRequest, "tool is required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), upstreamTimeout)
	defer cancel()
	var page proto.MCPApp
	if err := g.getJSON(ctx, "/workspaces/"+url.PathEscape(h.workspaceID())+"/mcp/app", url.Values{"tool": {tool}}, &page); err != nil {
		writeUpstreamError(w, err)
		return
	}
	hdr := w.Header()
	hdr.Set("Content-Type", "text/html; charset=utf-8")
	hdr.Set("Content-Security-Policy", mcpAppPolicy)
	hdr.Set("X-Frame-Options", "SAMEORIGIN")
	hdr.Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(mcpAppDocument(page.HTML)))
}
