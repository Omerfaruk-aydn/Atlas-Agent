import { Popover, Switch } from "radix-ui";
import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { CSRF_HEADER, csrfToken, http } from "../../api/http";
import { t } from "../../lib/i18n";
import { IconArrowUp, IconAsk, IconCheck, IconChevron, IconDebug, IconMultitask, IconPaperclip, IconPlan, IconPlug, IconPlus, IconSearch, IconSparkle } from "../../primitives/icons";
import { useApp } from "../../stores/app";

export function MCPPanel({ onBack, manage = true }: { onBack?: () => void; manage?: boolean }) {
  const settings = useApp((s) => s.settings);
  const workspace = useApp((s) => s.workspace?.id ?? "");
  const load = useApp((s) => s.loadSettings);
  const toast = useApp((s) => s.toast);
  const openSettings = useApp((s) => s.openSettings);
  const [query, setQuery] = useState("");
  const [pending, setPending] = useState<string[]>([]);
  const authRequests = useRef(new Map<string, AbortController>());
  useEffect(() => {
    void load().catch((err) => toast(String(err), "danger"));
    const timer = setInterval(() => void load().catch(() => {}), 3000);
    return () => { clearInterval(timer); authRequests.current.forEach((controller) => controller.abort()); authRequests.current.clear(); };
  }, [load, toast]);
  const servers = (settings?.mcp ?? []).filter((m) => m.name.toLocaleLowerCase().includes(query.toLocaleLowerCase()));
  const change = async (name: string, enabled: boolean) => {
    setPending((current) => [...current, name]);
    try {
      await http.post(`/api/workspaces/${encodeURIComponent(workspace)}/mcp/${encodeURIComponent(name)}`, { enabled });
      await load();
    } catch (err) { toast(err instanceof Error ? err.message : String(err), "danger"); }
    finally { setPending((current) => current.filter((item) => item !== name)); }
  };
  const authenticate = async (name: string) => {
    if (authRequests.current.has(name)) return;
    const controller = new AbortController();
    authRequests.current.set(name, controller);
    setPending((current) => [...current, name]);
    const path = `/api/workspaces/${encodeURIComponent(workspace)}/mcp/${encodeURIComponent(name)}/auth`;
    let opened = "";
    let polling = false;
    const timer = setInterval(async () => {
      if (polling || controller.signal.aborted) return;
      polling = true;
      try {
        const response = await http.get<{ auth_url?: string }>(path);
        if (response.auth_url && response.auth_url !== opened && !controller.signal.aborted) {
          opened = response.auth_url;
          await window.atlas?.openExternal(opened);
        }
      } catch { /* The POST reports authentication errors. */ }
      finally { polling = false; }
    }, 800);
    try {
      const res = await fetch(path, { method: "POST", headers: { [CSRF_HEADER]: csrfToken() }, signal: controller.signal });
      if (!res.ok) { const data = await res.json(); throw new Error(data.error ?? res.statusText); }
      await load();
    } catch (err) { if (!controller.signal.aborted) toast(err instanceof Error ? err.message : String(err), "danger"); }
    finally { clearInterval(timer); authRequests.current.delete(name); setPending((current) => current.filter((item) => item !== name)); }
  };
  return <div className="mcp-panel">
    <div className="context-menu-search">
      {onBack ? <button type="button" className="icon-button" aria-label={t("compose.back")} onClick={onBack}><IconArrowUp className="back-icon" /></button> : <IconSearch />}
      <input autoFocus={!!onBack} value={query} onChange={(e) => setQuery(e.target.value)} placeholder={t("compose.mcpSearch")} aria-label={t("compose.mcpSearch")} />
      {manage && <button type="button" className="link-button" onClick={() => openSettings("integrations")}>{t("compose.manage")} ↗</button>}
    </div>
    <div className="context-menu-list">
      {servers.map((server) => {
        const enabled = server.enabled ?? !/disabled/i.test(server.state);
        const failed = !!server.error || /error|failed|auth/i.test(server.state);
        const needsAuth = /auth|unauthorized|401/i.test(`${server.state} ${server.error ?? ""}`);
        return <div className="mcp-server" key={server.name}>
          <IconPlug />
          <span className="mcp-server-copy"><span>{server.name}</span><span className="mcp-server-status" data-error={failed || undefined} title={server.error}>{server.error || (server.tools ? `${server.tools} ${t("settings.tools")}` : server.state)}</span></span>
          {failed && enabled && <button type="button" className="link-button" disabled={pending.includes(server.name)} onClick={() => void (needsAuth ? authenticate(server.name) : change(server.name, true))}>{t(needsAuth ? "compose.authenticate" : "compose.connect")}</button>}
          <Switch.Root className="mcp-switch" checked={enabled} disabled={pending.includes(server.name)} aria-label={`${server.name}: ${t("compose.enabled")}`} onCheckedChange={(value) => void change(server.name, value)}><Switch.Thumb /></Switch.Root>
        </div>;
      })}
      {servers.length === 0 && <div className="context-menu-empty"><IconPlug /><strong>{t(query ? "palette.empty" : "compose.mcpEmpty")}</strong>{!query && <p>{t("compose.mcpHint")}</p>}</div>}
    </div>
  </div>;
}

export type WorkMode = "agent" | "plan" | "debug" | "multi" | "ask";

export const WORK_MODES = [
  { id: "plan", label: "compose.plan", hint: "compose.planHint", icon: IconPlan },
  { id: "debug", label: "compose.debug", hint: "compose.debugHint", icon: IconDebug },
  { id: "multi", label: "compose.multi", hint: "compose.multiHint", icon: IconMultitask },
  { id: "ask", label: "compose.ask", hint: "compose.askHint", icon: IconAsk },
] as const;

export function ContextMenu({ attach, commands, onMode, mode }: { attach(): void; commands(): void; onMode(mode: WorkMode): void; mode: WorkMode }) {
  const [open, setOpen] = useState(false);
  const [tab, setTab] = useState<"main" | "mcp">("main");
  const [query, setQuery] = useState("");
  const [width, setWidth] = useState(600);
  const trigger = useRef<HTMLButtonElement>(null);
  const input = useRef<HTMLInputElement>(null);
  const sessions = useApp((s) => s.sessions);
  const selectSession = useApp((s) => s.selectSession);
  useLayoutEffect(() => {
    const composer = trigger.current?.closest(".composer");
    if (!composer) return;
    const observer = new ResizeObserver(() => setWidth(composer.clientWidth));
    observer.observe(composer);
    return () => observer.disconnect();
  }, []);
  const close = (action: () => void) => { setOpen(false); action(); };
  return <Popover.Root open={open} onOpenChange={(value) => { setOpen(value); if (value) { setTab("main"); setQuery(""); } }}>
    <Popover.Trigger asChild><button ref={trigger} type="button" className="composer-add" aria-label={t("compose.add")}><IconPlus /></button></Popover.Trigger>
    <Popover.Portal><Popover.Content className="composer-menu context-menu" side="top" align="start" sideOffset={12} collisionPadding={{ top: 48, bottom: 12, left: 12, right: 12 }} style={{ width: Math.min(width, innerWidth - 24) }} aria-label={t("compose.add")} onOpenAutoFocus={(e) => { e.preventDefault(); input.current?.focus(); }} onKeyDown={(event) => {
      if (event.target === input.current && (event.key === "ArrowDown" || event.key === "Enter")) {
        event.preventDefault();
        const first = event.currentTarget.querySelector<HTMLButtonElement>(".context-menu-row");
        if (event.key === "Enter") first?.click();
        else first?.focus();
      }
    }}>
      {tab === "mcp" ? <MCPPanel onBack={() => setTab("main")} /> : <>
        <label className="context-menu-search"><IconSearch /><input ref={input} value={query} onChange={(e) => setQuery(e.target.value)} placeholder={t("compose.search")} aria-label={t("compose.search")} /></label>
        <div className="context-menu-list" onKeyDown={(event) => {
          if (event.key !== "ArrowDown" && event.key !== "ArrowUp") return;
          event.preventDefault();
          const rows = [...event.currentTarget.querySelectorAll<HTMLButtonElement>("button:not(:disabled)")];
          const at = rows.indexOf(document.activeElement as HTMLButtonElement);
          rows[(at + (event.key === "ArrowDown" ? 1 : -1) + rows.length) % rows.length]?.focus();
        }}>
          {WORK_MODES.filter((item) => `${t(item.label)} ${t(item.hint)}`.toLocaleLowerCase().includes(query.toLocaleLowerCase())).map((item) => <button type="button" className="context-menu-row" data-mode={item.id} key={item.id} onClick={() => close(() => onMode(item.id))}><item.icon /><span>{t(item.label)}</span><span className="context-row-hint">{t(item.hint)}</span>{mode === item.id && <IconCheck />}</button>)}
          {!query && <div className="context-menu-divider" />}
          {!query && <><button type="button" className="context-menu-row" onClick={() => close(attach)}><IconPaperclip /><span>{t("compose.attach")}</span></button><button type="button" className="context-menu-row" onClick={() => close(commands)}><IconSparkle /><span>{t("compose.commands")}</span><span className="context-row-hint">/</span></button><button type="button" className="context-menu-row" onClick={() => setTab("mcp")}><IconPlug /><span>MCP</span><IconChevron className="context-row-end" /></button></>}
          {query && sessions.filter((session) => session.title.toLocaleLowerCase().includes(query.toLocaleLowerCase())).slice(0, 5).map((session) => <button type="button" className="context-menu-row" key={session.id} onClick={() => close(() => void selectSession(session.id))}><IconSearch /><span className="context-row-title">{session.title}</span></button>)}
        </div>
      </>}
    </Popover.Content></Popover.Portal>
  </Popover.Root>;
}
