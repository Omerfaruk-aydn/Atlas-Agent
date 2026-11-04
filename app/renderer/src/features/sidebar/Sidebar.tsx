import { memo, useEffect, useMemo, useRef, useState, type KeyboardEvent } from "react";
import type { Session } from "../../api/types";
import { relative, toMs } from "../../lib/format";
import { t, type Key } from "../../lib/i18n";
import { useKeys } from "../../lib/shortcuts";
import { IconArchive, IconChevron, IconFolder, IconGear, IconMoon, IconPencil, IconPin, IconPlus, IconSearch, IconSun, IconTrash } from "../../primitives/icons";
import { Collapse } from "../../primitives/motion";
import { Hover, Kbd, Modal, ResizeHandle, RightClick, Tip, useDebounced } from "../../primitives/ui";
import { useApp } from "../../stores/app";

const DAY = 86_400_000;
const SHOW_LIMIT = 20;

function groupOf(s: Session): Key {
  const age = Date.now() - toMs(s.updated_at);
  if (age < DAY) return "sidebar.today";
  if (age < 7 * DAY) return "sidebar.week";
  return "sidebar.older";
}

function SessionCard({ session }: { session: Session }) {
  const project = useApp((s) => s.workspace?.path ?? "");
  const tokens = session.prompt_tokens + session.completion_tokens;
  return (
    <div className="session-card">
      <div className="session-card-title">{session.title}</div>
      <div className="session-card-line">{project}</div>
      <div className="session-card-line">
        {session.message_count} {t("sidebar.messages")}
        {tokens > 0 && ` · ${Math.round(tokens / 1000)}k token`}
        {session.cost > 0 && ` · $${session.cost.toFixed(2)}`}
      </div>
    </div>
  );
}

function RenameInput({ session, onDone }: { session: Session; onDone(): void }) {
  const rename = useApp((s) => s.renameSession);
  const [value, setValue] = useState(session.title);
  const input = useRef<HTMLInputElement>(null);
  useEffect(() => input.current?.select(), []);
  const commit = () => {
    if (value.trim() && value !== session.title) void rename(session.id, value);
    onDone();
  };
  const onKey = (e: KeyboardEvent) => {
    if (e.key === "Enter") commit();
    if (e.key === "Escape") {
      e.stopPropagation();
      onDone();
    }
  };
  return <input ref={input} className="rename" autoFocus value={value} aria-label={t("sidebar.rename")} onChange={(e) => setValue(e.target.value)} onBlur={commit} onKeyDown={onKey} />;
}

const Row = memo(function Row({ session, current, onDelete }: { session: Session; current: boolean; onDelete(session: Session): void }) {
  const busy = useApp((s) => !!s.busy[session.id]);
  const unread = useApp((s) => !!s.unread[session.id]);
  const waiting = useApp((s) => s.permissions.some((p) => p.session_id === session.id) || s.questions.some((q) => q.session_id === session.id));
  const pinned = useApp((s) => s.ui.pinned.includes(session.id));
  const archived = useApp((s) => s.ui.archived.includes(session.id));
  const select = useApp((s) => s.selectSession);
  const togglePin = useApp((s) => s.togglePin);
  const toggleArchive = useApp((s) => s.toggleArchive);
  const [renaming, setRenaming] = useState(false);
  const state = waiting ? "waiting" : busy ? "busy" : unread ? "unread" : undefined;

  const items = [
    { label: t("sidebar.rename"), icon: <IconPencil />, onSelect: () => setRenaming(true) },
    { label: pinned ? t("sidebar.unpin") : t("sidebar.pin"), icon: <IconPin />, onSelect: () => togglePin(session.id) },
    { label: archived ? t("sidebar.unarchive") : t("sidebar.archive"), icon: <IconArchive />, onSelect: () => toggleArchive(session.id) },
    { label: t("sidebar.delete"), icon: <IconTrash />, danger: true, onSelect: () => onDelete(session) },
  ];

  if (renaming) {
    return (
      <div className="session-row" data-current={current || undefined}>
        <RenameInput session={session} onDone={() => setRenaming(false)} />
      </div>
    );
  }

  return (
    <RightClick items={items}>
      <div className="session-row" data-current={current || undefined}>
        <Hover card={<SessionCard session={session} />}>
          <button type="button" className="session-main" aria-current={current || undefined} onClick={() => void select(session.id)} onDoubleClick={() => setRenaming(true)}>
            {state && <span className="dot" data-state={state} />}
            <span className="session-title">{session.title}</span>
            <span className="session-time">{relative(session.updated_at)}</span>
          </button>
        </Hover>
        <div className="session-tools">
          <Tip label={pinned ? t("sidebar.unpin") : t("sidebar.pin")}>
            <button type="button" className="icon-button" data-size="sm" data-on={pinned || undefined} aria-label={pinned ? t("sidebar.unpin") : t("sidebar.pin")} onClick={() => togglePin(session.id)}>
              <IconPin />
            </button>
          </Tip>
          <Tip label={archived ? t("sidebar.unarchive") : t("sidebar.archive")}>
            <button type="button" className="icon-button" data-size="sm" aria-label={archived ? t("sidebar.unarchive") : t("sidebar.archive")} onClick={() => toggleArchive(session.id)}>
              <IconArchive />
            </button>
          </Tip>
        </div>
      </div>
    </RightClick>
  );
});

const GROUPS: Key[] = ["sidebar.today", "sidebar.week", "sidebar.older"];

function Group({ label, items, currentId, onDelete }: { label: string; items: Session[]; currentId: string | null; onDelete(s: Session): void }) {
  const [all, setAll] = useState(false);
  const shown = all ? items : items.slice(0, SHOW_LIMIT);
  return (
    <div>
      <div className="group-label">{label}</div>
      {shown.map((s) => (
        <Row key={s.id} session={s} current={s.id === currentId} onDelete={onDelete} />
      ))}
      {items.length > shown.length && (
        <button type="button" className="show-all" onClick={() => setAll(true)}>
          {t("sidebar.showAll")} ({items.length})
        </button>
      )}
    </div>
  );
}

export function Sidebar() {
  const sessions = useApp((s) => s.sessions);
  const currentId = useApp((s) => s.currentId);
  const select = useApp((s) => s.selectSession);
  const pinned = useApp((s) => s.ui.pinned);
  const archived = useApp((s) => s.ui.archived);
  const openSettings = useApp((s) => s.openSettings);
  const dark = useApp((s) => s.scheme === "dark");
  const toggleTheme = useApp((s) => s.toggleTheme);
  const newChatKeys = useKeys("new");
  const settingsKeys = useKeys("settings");
  const search = useApp((s) => s.searchSessions);
  const remove = useApp((s) => s.deleteSession);
  const setUI = useApp((s) => s.setUI);
  const width = useApp((s) => s.ui.sidebarW);
  const [query, setQuery] = useState("");
  const [results, setResults] = useState<Session[] | null>(null);
  const [showArchive, setShowArchive] = useState(false);
  const [deleting, setDeleting] = useState<Session | null>(null);

  useDebounced(query.trim(), 180, (q) => {
    if (!q) {
      setResults(null);
      return;
    }
    search(q)
      .then(setResults)
      .catch(() => setResults([]));
  });

  const { pins, groups, archive } = useMemo(() => {
    const hidden = new Set(archived);
    const pinSet = new Set(pinned);
    const live = sessions.filter((s) => !hidden.has(s.id));
    const rest = live.filter((s) => !pinSet.has(s.id));
    return {
      pins: pinned.flatMap((id) => live.filter((s) => s.id === id)),
      groups: GROUPS.map((key) => ({ key, items: rest.filter((s) => groupOf(s) === key) })).filter((g) => g.items.length > 0),
      archive: sessions.filter((s) => hidden.has(s.id)),
    };
  }, [sessions, pinned, archived]);

  const openFolder = async () => {
    const folder = await window.atlas?.pickFolder();
    if (folder) await window.atlas?.openProject(folder);
  };

  const empty = pins.length === 0 && groups.length === 0;

  return (
    <nav className="sidebar" aria-label="Atlas">
      <button type="button" className="new-chat" data-current={currentId === null || undefined} onClick={() => void select(null)}>
        <IconPlus />
        <span>{t("sidebar.new")}</span>
        {newChatKeys && <Kbd keys={newChatKeys} />}
      </button>
      <label className="search">
        <IconSearch />
        <input value={query} placeholder={t("sidebar.search")} aria-label={t("sidebar.search")} onChange={(e) => setQuery(e.target.value)} />
      </label>
      <div className="session-list">
        {results ? (
          results.length > 0 ? (
            <Group label={t("sidebar.results")} items={results} currentId={currentId} onDelete={setDeleting} />
          ) : (
            <div className="sidebar-empty">{t("sidebar.noResults")}</div>
          )
        ) : (
          <>
            {pins.length > 0 && <Group label={t("sidebar.pinned")} items={pins} currentId={currentId} onDelete={setDeleting} />}
            {groups.map((group) => (
              <Group key={group.key} label={t(group.key)} items={group.items} currentId={currentId} onDelete={setDeleting} />
            ))}
            {empty && <div className="sidebar-empty">{t("sidebar.empty")}</div>}
            {archive.length > 0 && (
              <div>
                <button type="button" className="group-toggle" aria-expanded={showArchive} onClick={() => setShowArchive(!showArchive)}>
                  <IconChevron className="chevron" />
                  {t("sidebar.archived")} ({archive.length})
                </button>
                <Collapse open={showArchive}>
                  {showArchive && archive.map((s) => <Row key={s.id} session={s} current={s.id === currentId} onDelete={setDeleting} />)}
                </Collapse>
              </div>
            )}
          </>
        )}
      </div>
      <div className="sidebar-foot">
        <Tip label={t("sidebar.openFolder")}>
          <button type="button" className="icon-button" aria-label={t("sidebar.openFolder")} onClick={() => void openFolder()}>
            <IconFolder />
          </button>
        </Tip>
        <Tip label={t(dark ? "theme.toLight" : "theme.toDark")}>
          <button type="button" className="icon-button" aria-label={t(dark ? "theme.toLight" : "theme.toDark")} onClick={toggleTheme}>
            {dark ? <IconSun /> : <IconMoon />}
          </button>
        </Tip>
        <Tip label={t("sidebar.settings")} keys={settingsKeys}>
          <button type="button" className="icon-button" aria-label={t("sidebar.settings")} onClick={() => openSettings()}>
            <IconGear />
          </button>
        </Tip>
      </div>
      <ResizeHandle side="left" min={220} max={360} initial={width} cssVar="--atlas-sidebar-w" onDone={(w) => setUI("sidebarW", w)} />
      <Modal open={!!deleting} onClose={() => setDeleting(null)} title={t("sidebar.deleteTitle")} description={deleting?.title} className="modal-sm">
        <p className="modal-text">{t("sidebar.deleteBody")}</p>
        <div className="modal-actions">
          <button type="button" className="button" onClick={() => setDeleting(null)}>
            {t("common.cancel")}
          </button>
          <button
            type="button"
            className="button"
            data-variant="danger"
            onClick={() => {
              if (deleting) void remove(deleting.id);
              setDeleting(null);
            }}
          >
            {t("sidebar.delete")}
          </button>
        </div>
      </Modal>
    </nav>
  );
}
