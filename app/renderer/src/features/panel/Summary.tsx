// The control at the top right of a thread: a small popover with what this
// chat has changed, its side chats, the agents it started, and the sources it
// used. A section with nothing in it is not shown.

import { Popover } from "radix-ui";
import { useMemo, useState } from "react";
import type { Message } from "../../api/types";
import { baseName } from "../../lib/format";
import { t } from "../../lib/i18n";
import { hostOf, SEARCH_URL } from "../../lib/panel";
import { useKeys } from "../../lib/shortcuts";
import { category, toolFile } from "../../lib/tools";
import { IconChat, IconCopy, IconDiff, IconFile, IconFolder, IconGlobe, IconLink, IconMore, IconPlus, IconSummary, IconTerminal } from "../../primitives/icons";
import { Menu, Modal, Tip } from "../../primitives/ui";
import { sideOf, useApp } from "../../stores/app";
import { usePanel } from "../../stores/panel";
import { SiteFavicon } from "./SiteIcon";
import { useSessionChanges } from "./Tools";

const NO_MESSAGES: Message[] = [];
const SOURCES_SHOWN = 4;
const SOURCES_MAX = 60;

type Source =
  | { key: string; kind: "file"; name: string; path: string }
  | { key: string; kind: "attachment"; name: string; mime: string; data: string }
  | { key: string; kind: "link"; name: string; url: string }
  | { key: string; kind: "search"; name: string; query: string };

function field(input: string, name: string): string {
  try {
    const value = (JSON.parse(input) as Record<string, unknown>)[name];
    return typeof value === "string" ? value : "";
  } catch {
    return "";
  }
}

interface Gathered {
  sources: Source[];
  agents: { done: number; running: number };
}

/** Attachments, files the agent worked with and pages it read, newest first; and the agents it started. */
export function useGathered(): Gathered {
  const messages = useApp((s) => (s.currentId ? (s.messages[s.currentId] ?? NO_MESSAGES) : NO_MESSAGES));
  return useMemo(() => {
    const sources: Source[] = [];
    const seen = new Set<string>();
    let done = 0;
    let running = 0;
    let search = false;
    for (let i = messages.length - 1; i >= 0; i--) {
      const message = messages[i]!;
      const answered = new Set(message.parts.flatMap((p) => (p.type === "tool_result" ? [p.data.tool_call_id] : [])));
      for (let j = message.parts.length - 1; j >= 0; j--) {
        const part = message.parts[j]!;
        if (part.type === "image_url" && message.role === "user") {
          sources.push({ key: `${message.id}:${j}`, kind: "attachment", name: t("summary.image"), mime: "image/png", data: part.data.url });
          continue;
        }
        if (part.type === "binary" && message.role === "user" && part.data.Data) {
          const name = (part.data.Path ?? "").split(/[\\/]/).pop() || part.data.MIMEType || t("summary.file");
          sources.push({ key: `${message.id}:${j}`, kind: "attachment", name, mime: part.data.MIMEType ?? "", data: `data:${part.data.MIMEType ?? "application/octet-stream"};base64,${part.data.Data}` });
          continue;
        }
        if (part.type !== "tool_call") continue;
        const cat = category(part.data.name);
        if (cat === "agent") {
          if (answered.has(part.data.id)) done++;
          else running++;
        } else if (cat === "fetch") {
          const url = field(part.data.input, "url");
          if (/^https?:\/\//i.test(url) && !seen.has(url)) {
            seen.add(url);
            sources.push({ key: url, kind: "link", name: url.replace(/^https?:\/\//i, ""), url });
          }
        } else if (cat === "web") {
          const query = field(part.data.input, "query");
          if (!search && query) {
            search = true;
            sources.push({ key: `search:${query}`, kind: "search", name: t("summary.webSearch"), query });
          }
        } else if (cat === "read" || cat === "edit" || cat === "write") {
          const path = toolFile(part.data);
          if (path && !seen.has(path)) {
            seen.add(path);
            sources.push({ key: path, kind: "file", name: baseName(path), path });
          }
        }
      }
    }
    return { sources: sources.slice(0, SOURCES_MAX), agents: { done, running } };
  }, [messages]);
}

function Thumb({ source }: { source: Source }) {
  if (source.kind === "attachment" && source.mime.startsWith("image/")) return <img className="source-thumb" src={source.data} alt="" draggable={false} />;
  if (source.kind === "link") return <SiteFavicon host={hostOf(source.url)} />;
  if (source.kind === "search") return <IconGlobe className="source-icon" />;
  return <IconFile className="source-icon" />;
}

/** An attachment of the chat, shown as it was sent. */
function AttachmentPreview({ source, onClose }: { source: Extract<Source, { kind: "attachment" }> | null; onClose(): void }) {
  const text = useMemo(() => {
    if (!source || source.mime.startsWith("image/")) return "";
    try {
      return new TextDecoder().decode(Uint8Array.from(atob(source.data.split(",")[1] ?? ""), (c) => c.charCodeAt(0)));
    } catch {
      return "";
    }
  }, [source]);
  return (
    <Modal open={!!source} onClose={onClose} title={source?.name ?? ""} className="modal-wide">
      {source?.mime.startsWith("image/") ? <img className="preview-image" src={source.data} alt={source.name} /> : <pre className="preview-text">{text || t("rail.binary")}</pre>}
      <div className="modal-actions">
        <button type="button" className="button" onClick={onClose}>
          {t("common.close")}
        </button>
      </div>
    </Modal>
  );
}

export function SummaryButton() {
  const [open, setOpen] = useState(false);
  const [all, setAll] = useState(false);
  const [preview, setPreview] = useState<Extract<Source, { kind: "attachment" }> | null>(null);
  const workspace = useApp((s) => s.workspace);
  const currentId = useApp((s) => s.currentId);
  const sideChats = useApp((s) => s.sideChats);
  const changes = useSessionChanges();
  const { sources, agents } = useGathered();
  const openTool = usePanel((s) => s.openTool);
  const openFile = usePanel((s) => s.openFile);
  const openUrl = usePanel((s) => s.openUrl);
  const openSideChat = usePanel((s) => s.openSideChat);
  const addTab = usePanel((s) => s.addTab);
  const toast = useApp((s) => s.toast);
  const keys = useKeys("summary");

  const mine = useMemo(() => sideChats.filter((s) => sideOf(s) === currentId), [sideChats, currentId]);
  const additions = changes.reduce((n, c) => n + c.additions, 0);
  const removals = changes.reduce((n, c) => n + c.removals, 0);
  const shown = all ? sources : sources.slice(0, SOURCES_SHOWN);
  const close = () => setOpen(false);

  const choose = (source: Source) => {
    if (source.kind === "attachment") return setPreview(source);
    close();
    if (source.kind === "file") openFile(source.path);
    else if (source.kind === "link") openUrl(source.url);
    else openUrl(SEARCH_URL + encodeURIComponent(source.query));
  };

  return (
    <>
      <Popover.Root
        open={open}
        onOpenChange={(next) => {
          setOpen(next);
          if (!next) setAll(false);
        }}
      >
        <Tip label={t("summary.open")} keys={keys} hidden={open}>
          <Popover.Trigger asChild>
            <button type="button" className="icon-button" data-summary-trigger data-on={open || undefined} aria-label={t("summary.open")}>
              <IconSummary />
            </button>
          </Popover.Trigger>
        </Tip>
        <Popover.Portal>
          <Popover.Content className="pop summary-pop" side="bottom" align="end" sideOffset={4} collisionPadding={8} aria-label={t("summary.open")} onKeyDown={(e) => {
              // After a menu inside it closed, Radix can still hold the first Esc: it must close the popover.
              if (e.key === "Escape" && !(e.target as HTMLElement).closest(".menu")) close();
            }}>
            <div className="summary-head">
              <span className="summary-project" title={workspace?.path}>
                {workspace?.name}
              </span>
              <Menu
                modal={false}
                side="left"
                align="start"
                trigger={
                  <button type="button" className="icon-button" data-size="sm" aria-label={t("summary.menu")}>
                    <IconMore />
                  </button>
                }
                items={[
                  {
                    label: t("summary.terminal"),
                    icon: <IconTerminal />,
                    onSelect: () => {
                      close();
                      addTab({ kind: "terminal" });
                    },
                  },
                  { label: t("summary.reveal"), icon: <IconFolder />, onSelect: () => workspace && void window.atlas?.reveal(workspace.path) },
                  {
                    label: t("summary.copyPath"),
                    icon: <IconCopy />,
                    onSelect: () => workspace && void navigator.clipboard.writeText(workspace.path).then(() => toast(t("panel.copied"))),
                  },
                ]}
              />
            </div>

            <button
              type="button"
              className="summary-row summary-changes"
              onClick={() => {
                openTool("changes");
                close();
              }}
            >
              <IconDiff />
              <span className="summary-name">{t("rail.changes")}</span>
              {changes.length > 0 ? (
                <span className="summary-counts">
                  {additions > 0 && <span className="add">+{additions}</span>}
                  {removals > 0 && <span className="del">−{removals}</span>}
                  {additions === 0 && removals === 0 && <span className="summary-muted">{changes.length}</span>}
                </span>
              ) : (
                <span className="summary-muted">{t("summary.none")}</span>
              )}
            </button>

            {mine.length > 0 && (
              <>
                <div className="summary-section">
                  <h3>{t("summary.side")}</h3>
                </div>
                {mine.map((chat) => (
                  <button
                    key={chat.id}
                    type="button"
                    className="summary-row"
                    onClick={() => {
                      openSideChat(chat.id);
                      close();
                    }}
                  >
                    <IconChat />
                    <span className="summary-name">{chat.title.replace(/^Branch: /, "")}</span>
                    {chat.is_busy && <span className="dot" data-state="busy" />}
                  </button>
                ))}
              </>
            )}

            {agents.done + agents.running > 0 && (
              <>
                <div className="summary-section">
                  <h3>{t("summary.agents")}</h3>
                </div>
                <div className="summary-row summary-static">
                  <span className="agent-dots" aria-hidden="true">
                    {Array.from({ length: Math.min(4, agents.done + agents.running) }, (_, i) => (
                      <span key={i} className="agent-dot" data-running={i < agents.running || undefined} />
                    ))}
                  </span>
                  <span className="summary-name">
                    {agents.running > 0 ? `${agents.running} ${t("summary.agentsRunning")}` : ""}
                    {agents.running > 0 && agents.done > 0 ? ", " : ""}
                    {agents.done > 0 ? `${agents.done} ${t("summary.agentsDone")}` : ""}
                  </span>
                </div>
              </>
            )}

            {sources.length > 0 && (
              <>
                <div className="summary-section">
                  <h3>{t("summary.sources")}</h3>
                  <Tip label={t("summary.addSource")}>
                    <button
                      type="button"
                      className="icon-button"
                      data-size="sm"
                      aria-label={t("summary.addSource")}
                      onClick={() => {
                        close();
                        document.querySelector<HTMLInputElement>(".composer input[type=file]")?.click();
                      }}
                    >
                      <IconPlus />
                    </button>
                  </Tip>
                </div>
                <div className="summary-sources" data-all={all || undefined}>
                  {shown.map((source) => (
                    <button key={source.key} type="button" className="summary-row" title={source.kind === "file" ? source.path : source.kind === "link" ? source.url : source.name} onClick={() => choose(source)}>
                      <Thumb source={source} />
                      <span className="summary-name">{source.name}</span>
                    </button>
                  ))}
                </div>
                {sources.length > SOURCES_SHOWN && (
                  <button type="button" className="summary-row summary-all" onClick={() => setAll(!all)}>
                    <IconLink />
                    <span className="summary-name">{all ? t("summary.less") : t("summary.viewAll")}</span>
                  </button>
                )}
              </>
            )}
          </Popover.Content>
        </Popover.Portal>
      </Popover.Root>
      <AttachmentPreview source={preview} onClose={() => setPreview(null)} />
    </>
  );
}
