// Ctrl+K: one box for chats, actions, models and project files. Ctrl+P opens
// the same box limited to chats. Everything works from the keyboard.

import fuzzysort from "fuzzysort";
import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { http } from "../../api/http";
import { baseName, relative } from "../../lib/format";
import { t, type Key } from "../../lib/i18n";
import { keysOf, SHORTCUTS } from "../../lib/shortcuts";
import { IconChevron, IconFile, IconFolder, IconGear, IconMoon, IconPanelRight, IconPlus, IconSearch, IconSidebar, IconSparkle, IconSun } from "../../primitives/icons";
import { Kbd, Modal, useDebounced } from "../../primitives/ui";
import { useApp } from "../../stores/app";

interface Item {
  id: string;
  group: Key;
  title: string;
  hint?: string;
  keys?: string;
  icon: ReactNode;
  run(): void;
}

type Hit = NonNullable<ReturnType<typeof fuzzysort.single>>;

const MAX_PER_GROUP = 7;
const GROUP_ORDER: Key[] = ["palette.chats", "palette.actions", "palette.files", "palette.models"];

function highlight(result: Hit | undefined, fallback: string): ReactNode {
  if (!result) return fallback;
  return result.highlight((m, i) => <b key={i}>{m}</b>);
}

function useItems(chatsOnly: boolean, files: string[]) {
  const sessions = useApp((s) => s.sessions);
  const archived = useApp((s) => s.ui.archived);
  const providers = useApp((s) => s.catalog?.providers);
  const overrides = useApp((s) => s.ui.shortcuts);

  return useMemo(() => {
    const app = useApp.getState;
    const chats: Item[] = sessions
      .filter((s) => !archived.includes(s.id))
      .map((s) => ({ id: `chat:${s.id}`, group: "palette.chats", title: s.title, hint: relative(s.updated_at), icon: <IconChevron />, run: () => void app().selectSession(s.id) }));
    if (chatsOnly) return chats;

    const keysFor = (id: string) => {
      const shortcut = SHORTCUTS.find((s) => s.id === id);
      return shortcut ? keysOf(shortcut, overrides)[0] : undefined;
    };
    const action = (id: string, title: Key, icon: ReactNode, run: () => void): Item => ({ id, group: "palette.actions", title: t(title), keys: keysFor(id), icon, run });
    const actions = [
      action("new", "shortcut.new", <IconPlus />, () => void app().selectSession(null)),
      action("sidebar", "shortcut.sidebar", <IconSidebar />, () => app().toggleSidebar()),
      action("rail", "shortcut.rail", <IconPanelRight />, () => app().toggleRail()),
      action("models", "shortcut.models", <IconSparkle />, () => app().openDialog("models")),
      action("settings", "shortcut.settings", <IconGear />, () => app().openSettings()),
      action("shortcuts", "shortcut.help", <IconGear />, () => app().openSettings("shortcuts")),
      action("compact", "context.compact", <IconSparkle />, () => void app().summarize()),
      action("light", "palette.light", <IconSun />, () => app().setTheme("light")),
      action("dark", "palette.dark", <IconMoon />, () => app().setTheme("dark")),
      action("folder", "sidebar.openFolder", <IconFolder />, () => {
        void window.atlas?.pickFolder().then((f) => {
          if (f) void window.atlas?.openProject(f);
        });
      }),
    ];
    const models: Item[] = (providers ?? []).flatMap((p) =>
      p.models.map((m) => ({ id: `model:${p.id}:${m.id}`, group: "palette.models" as Key, title: m.name, hint: p.name, icon: <IconSparkle />, run: () => void app().selectModel(p.id, m.id) })),
    );
    const fileItems: Item[] = files.map((f) => ({ id: `file:${f}`, group: "palette.files", title: baseName(f), hint: f, icon: <IconFile />, run: () => app().openFile(f) }));
    return [...chats, ...actions, ...fileItems, ...models];
  }, [sessions, archived, providers, files, chatsOnly, overrides]);
}

export function Palette() {
  const dialog = useApp((s) => s.dialog);
  const close = useApp((s) => s.openDialog);
  const workspaceId = useApp((s) => s.workspace?.id ?? "");
  const open = dialog === "palette" || dialog === "sessions";
  const chatsOnly = dialog === "sessions";
  const [query, setQuery] = useState("");
  const [files, setFiles] = useState<string[]>([]);
  const [selected, setSelected] = useState(0);
  const list = useRef<HTMLDivElement>(null);
  const items = useItems(chatsOnly, files);

  useEffect(() => {
    if (open) {
      setQuery("");
      setFiles([]);
      setSelected(0);
    }
  }, [open]);

  useDebounced(open && !chatsOnly ? query.trim() : "", 120, (q) => {
    if (q.length < 2 || !workspaceId) {
      setFiles([]);
      return;
    }
    http
      .get<{ files: string[] }>(`/api/workspaces/${encodeURIComponent(workspaceId)}/files/search?limit=${MAX_PER_GROUP}&q=${encodeURIComponent(q)}`)
      .then((r) => setFiles(r.files ?? []))
      .catch(() => setFiles([]));
  });

  const shown = useMemo(() => {
    const q = query.trim();
    const entries: { item: Item; result?: Hit }[] = [];
    const take = (group: Key, list: Item[]) => {
      if (group === "palette.files") {
        for (const item of list.slice(0, MAX_PER_GROUP)) entries.push({ item });
        return;
      }
      if (!q) {
        // With nothing typed, models stay out of the way.
        if (group !== "palette.models") for (const item of list.slice(0, MAX_PER_GROUP)) entries.push({ item });
        return;
      }
      for (const hit of fuzzysort.go(q, list, { key: "title", limit: MAX_PER_GROUP, threshold: 0.3 })) {
        entries.push({ item: hit.obj, result: hit });
      }
    };
    for (const group of GROUP_ORDER) take(group, items.filter((i) => i.group === group));
    return entries;
  }, [items, query]);

  useEffect(() => setSelected(0), [query]);
  useEffect(() => {
    list.current?.querySelector('[data-selected="true"]')?.scrollIntoView({ block: "nearest" });
  }, [selected]);

  const run = (item: Item | undefined) => {
    if (!item) return;
    close(null);
    item.run();
  };

  let lastGroup: Key | null = null;

  return (
    <Modal open={open} onClose={() => close(null)} title={t(chatsOnly ? "shortcut.sessions" : "shortcut.palette")} hideTitle className="palette" >
      <label className="palette-input">
        <IconSearch />
        <input
          autoFocus
          value={query}
          placeholder={t(chatsOnly ? "palette.searchChats" : "palette.search")}
          aria-label={t("palette.search")}
          role="combobox"
          aria-expanded="true"
          aria-controls="palette-list"
          onChange={(e) => setQuery(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "ArrowDown" || e.key === "ArrowUp") {
              e.preventDefault();
              const step = e.key === "ArrowDown" ? 1 : -1;
              if (shown.length) setSelected((selected + step + shown.length) % shown.length);
            } else if (e.key === "Enter") {
              e.preventDefault();
              run(shown[selected]?.item);
            }
          }}
        />
      </label>
      <div className="palette-list" id="palette-list" role="listbox" ref={list}>
        {shown.map(({ item, result }, i) => {
          const heading = item.group !== lastGroup ? item.group : null;
          lastGroup = item.group;
          return (
            <div key={item.id}>
              {heading && <div className="palette-group">{t(heading)}</div>}
              <button
                type="button"
                role="option"
                aria-selected={i === selected}
                data-selected={i === selected}
                className="palette-row"
                onMouseMove={() => setSelected(i)}
                onClick={() => run(item)}
              >
                {item.icon}
                <span className="palette-title">{highlight(result, item.title)}</span>
                {item.hint && <span className="palette-hint">{item.hint}</span>}
                {item.keys && <Kbd keys={item.keys} />}
              </button>
            </div>
          );
        })}
        {shown.length === 0 && <div className="palette-empty">{t("palette.empty")}</div>}
      </div>
    </Modal>
  );
}
