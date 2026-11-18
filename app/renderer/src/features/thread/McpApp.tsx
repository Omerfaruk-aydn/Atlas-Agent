// The interactive page an MCP tool can offer (an MCP App). It is third-party
// HTML, so it is shown in a frame that cannot reach the app: scripts may run
// inside it, but it gets no same-origin access, no network and no navigation.
//
// The page is served as its own document (see the gateway) with a policy that
// blocks every request it could make. The one way out is
// window.atlas.callTool(name, args), which the app answers
// by asking the server to run that tool on the page's own MCP server. The
// server refuses any tool that does not declare that pages may call it.

import { useEffect, useRef, useState } from "react";
import { http } from "../../api/http";
import { t } from "../../lib/i18n";
import { Modal } from "../../primitives/ui";
import { useApp } from "../../stores/app";

interface AppPage {
  server: string;
  tool: string;
  uri: string;
  html: string;
}

interface CallResult {
  content: string;
  is_error?: boolean;
}

/** Messages between the page and the app carry this marker, so nothing else is mistaken for them. */
const CALL = "atlas-mcp-call";
const RESULT = "atlas-mcp-result";

/** A call message from the page, or null when the message is anything else. */
export function parseCall(data: unknown): { id: number; tool: string; arguments: Record<string, unknown> } | null {
  if (!data || typeof data !== "object") return null;
  const m = data as Record<string, unknown>;
  if (m.type !== CALL || typeof m.id !== "number" || typeof m.tool !== "string" || !m.tool || m.tool.length > 200) return null;
  const args = m.arguments && typeof m.arguments === "object" && !Array.isArray(m.arguments) ? (m.arguments as Record<string, unknown>) : {};
  return { id: m.id, tool: m.tool, arguments: args };
}

function AppFrame({ page, workspace }: { page: AppPage; workspace: string }) {
  const frame = useRef<HTMLIFrameElement>(null);

  useEffect(() => {
    const onMessage = (event: MessageEvent) => {
      // Only the frame this component made, never another window.
      if (event.source !== frame.current?.contentWindow) return;
      const call = parseCall(event.data);
      if (!call) return;
      const reply = (body: { result?: CallResult; error?: string }) => frame.current?.contentWindow?.postMessage({ type: RESULT, id: call.id, ...body }, "*");
      http
        .post<CallResult>(`/api/workspaces/${encodeURIComponent(workspace)}/mcp-app/call`, { tool: `mcp_${page.server}_${page.tool}`, call: call.tool, arguments: call.arguments })
        .then((result) => reply({ result }))
        .catch((e: unknown) => reply({ error: e instanceof Error ? e.message : String(e) }));
    };
    window.addEventListener("message", onMessage);
    return () => window.removeEventListener("message", onMessage);
  }, [page, workspace]);

  // The page is its own document with its own strict policy: a srcDoc frame would inherit the app's, which stops its scripts.
  const src = `/api/workspaces/${encodeURIComponent(workspace)}/mcp-app/page?tool=${encodeURIComponent(`mcp_${page.server}_${page.tool}`)}`;
  return <iframe ref={frame} className="mcp-app-frame" title={page.tool} sandbox="allow-scripts" referrerPolicy="no-referrer" src={src} />;
}

export function McpAppButton({ tool }: { tool: string }) {
  const workspace = useApp((s) => s.workspace?.id);
  const [page, setPage] = useState<AppPage | null>(null);
  const [open, setOpen] = useState(false);

  // A tool without a page, or with apps switched off, shows nothing.
  useEffect(() => {
    if (!workspace || !tool.startsWith("mcp_")) return;
    let alive = true;
    http
      .get<AppPage>(`/api/workspaces/${encodeURIComponent(workspace)}/mcp-app?tool=${encodeURIComponent(tool)}`)
      .then((found) => alive && setPage(found))
      .catch(() => alive && setPage(null));
    return () => {
      alive = false;
    };
  }, [workspace, tool]);

  if (!page || !workspace) return null;
  return (
    <>
      <button type="button" className="button" data-action="open-mcp-app" onClick={() => setOpen(true)}>
        {t("mcpApp.open")}
      </button>
      <Modal open={open} onClose={() => setOpen(false)} title={`${page.server}: ${page.tool}`} className="modal-wide">
        {open && <AppFrame page={page} workspace={workspace} />}
        <div className="modal-actions">
          <button type="button" className="button" onClick={() => setOpen(false)}>
            {t("common.close")}
          </button>
        </div>
      </Modal>
    </>
  );
}
