// The left side of the design tool: pages, the layer tree (drag to reorder
// or nest, eye and lock toggles, double click to rename) and the assets
// (components to place as instances).

import { useState, type DragEvent as ReactDragEvent, type KeyboardEvent as ReactKeyboardEvent } from "react";
import { t } from "../../lib/i18n";
import { useDesign } from "../../stores/design";
import { components, type DesignNode, isContainer, locate, pageOf } from "./model";
import { CommentsPanel, HistoryPanel } from "./Review";
import { IconEye, IconEyeOff, IconLock, IconUnlock, TypeIcon } from "./icons";

function Rename({ value, onDone, label }: { value: string; onDone: (v: string | null) => void; label: string }) {
  const [text, setText] = useState(value);
  return (
    <input
      className="design-rename"
      aria-label={label}
      autoFocus
      value={text}
      onChange={(e) => setText(e.target.value)}
      onFocus={(e) => e.currentTarget.select()}
      onBlur={() => onDone(text)}
      onKeyDown={(e) => {
        e.stopPropagation();
        if (e.key === "Enter") onDone(text);
        if (e.key === "Escape") onDone(null);
      }}
    />
  );
}

function Pages() {
  const doc = useDesign((s) => s.doc);
  const pageId = useDesign((s) => s.pageId);
  const setPage = useDesign((s) => s.setPage);
  const addPage = useDesign((s) => s.addPage);
  const renamePage = useDesign((s) => s.renamePage);
  const removePage = useDesign((s) => s.removePage);
  const [renaming, setRenaming] = useState<string | null>(null);
  if (!doc) return null;
  return (
    <section className="design-section design-pages" aria-label={t("design.pages")}>
      <header className="design-section-head">
        <span>{t("design.pages")}</span>
        <button type="button" className="icon-button design-mini" aria-label={t("design.addPage")} title={t("design.addPage")} onClick={addPage}>
          +
        </button>
      </header>
      <ul role="listbox" aria-label={t("design.pages")}>
        {doc.pages.map((p) => (
          <li key={p.id} role="option" aria-selected={p.id === pageId} data-on={p.id === pageId || undefined} className="design-page-row" onClick={() => setPage(p.id)} onDoubleClick={() => setRenaming(p.id)}>
            {renaming === p.id ? (
              <Rename
                value={p.name}
                label={t("design.rename")}
                onDone={(v) => {
                  setRenaming(null);
                  if (v !== null) renamePage(p.id, v);
                }}
              />
            ) : (
              <span className="design-row-name">{p.name}</span>
            )}
            {doc.pages.length > 1 && (
              <button
                type="button"
                className="design-row-tool"
                aria-label={`${t("design.deletePage")}: ${p.name}`}
                onClick={(e) => {
                  e.stopPropagation();
                  removePage(p.id);
                }}
              >
                ×
              </button>
            )}
          </li>
        ))}
      </ul>
    </section>
  );
}

type Drop = { id: string; where: "above" | "below" | "inside" } | null;

function LayerRow({ node, depth, drop, setDrop, collapsed, toggle }: { node: DesignNode; depth: number; drop: Drop; setDrop: (d: Drop) => void; collapsed: Set<string>; toggle: (id: string) => void }) {
  const selection = useDesign((s) => s.selection);
  const select = useDesign((s) => s.select);
  const patch = useDesign((s) => s.patch);
  const hover = useDesign((s) => s.hover);
  const setHover = useDesign((s) => s.setHover);
  const [renaming, setRenaming] = useState(false);
  const selected = selection.includes(node.id);
  const kids = node.children ?? [];
  const open = !collapsed.has(node.id);

  const onClick = (e: React.MouseEvent) => {
    if (e.shiftKey || e.ctrlKey || e.metaKey) select(selected ? selection.filter((id) => id !== node.id) : [...selection, node.id]);
    else select([node.id]);
  };
  const onDragOver = (e: ReactDragEvent) => {
    if (!e.dataTransfer.types.includes("application/x-atlas-layer")) return;
    e.preventDefault();
    const box = (e.currentTarget as HTMLElement).getBoundingClientRect();
    const y = (e.clientY - box.top) / box.height;
    const where = isContainer(node) && y > 0.25 && y < 0.75 ? "inside" : y < 0.5 ? "above" : "below";
    if (drop?.id !== node.id || drop.where !== where) setDrop({ id: node.id, where });
  };
  return (
    <>
      <li
        className="design-layer"
        role="treeitem"
        aria-selected={selected}
        aria-expanded={kids.length ? open : undefined}
        aria-level={depth + 1}
        data-on={selected || undefined}
        data-hover={hover === node.id || undefined}
        data-hidden={node.visible === false || undefined}
        data-drop={drop?.id === node.id ? drop.where : undefined}
        data-type={node.type}
        style={{ ["--depth" as string]: depth }}
        draggable={!renaming}
        onDragStart={(e) => {
          e.dataTransfer.setData("application/x-atlas-layer", node.id);
          e.dataTransfer.effectAllowed = "move";
          if (!selected) select([node.id]);
        }}
        onDragOver={onDragOver}
        onDragLeave={() => drop?.id === node.id && setDrop(null)}
        onClick={onClick}
        onPointerEnter={() => setHover(node.id)}
        onPointerLeave={() => setHover(null)}
        onDoubleClick={() => setRenaming(true)}
      >
        <button
          type="button"
          className="design-twisty"
          aria-hidden={kids.length === 0}
          tabIndex={-1}
          data-open={open || undefined}
          style={{ visibility: kids.length ? "visible" : "hidden" }}
          onClick={(e) => {
            e.stopPropagation();
            toggle(node.id);
          }}
        >
          ▸
        </button>
        <TypeIcon type={node.type} />
        {renaming ? (
          <Rename
            value={node.name}
            label={t("design.rename")}
            onDone={(v) => {
              setRenaming(false);
              if (v !== null && v.trim() && v !== node.name) patch([node.id], { name: v.trim().slice(0, 200) });
            }}
          />
        ) : (
          <span className="design-row-name">{node.name}</span>
        )}
        <button
          type="button"
          className="design-row-tool"
          data-on={node.locked || undefined}
          aria-label={`${node.locked ? t("design.unlock") : t("design.lock")}: ${node.name}`}
          aria-pressed={node.locked === true}
          onClick={(e) => {
            e.stopPropagation();
            patch([node.id], { locked: !node.locked });
          }}
        >
          {node.locked ? <IconLock /> : <IconUnlock />}
        </button>
        <button
          type="button"
          className="design-row-tool"
          data-on={node.visible === false || undefined}
          aria-label={`${node.visible === false ? t("design.show") : t("design.hide")}: ${node.name}`}
          aria-pressed={node.visible === false}
          onClick={(e) => {
            e.stopPropagation();
            patch([node.id], { visible: node.visible === false });
          }}
        >
          {node.visible === false ? <IconEyeOff /> : <IconEye />}
        </button>
      </li>
      {open &&
        kids
          .slice()
          .reverse()
          .map((c) => <LayerRow key={c.id} node={c} depth={depth + 1} drop={drop} setDrop={setDrop} collapsed={collapsed} toggle={toggle} />)}
    </>
  );
}

function LayerTree() {
  const doc = useDesign((s) => s.doc);
  const pageId = useDesign((s) => s.pageId);
  const moveInto = useDesign((s) => s.moveInto);
  const selection = useDesign((s) => s.selection);
  const [drop, setDrop] = useState<Drop>(null);
  const [collapsed, setCollapsed] = useState<Set<string>>(new Set());
  if (!doc) return null;
  const page = pageOf(doc, pageId);
  const toggle = (id: string) =>
    setCollapsed((c) => {
      const next = new Set(c);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  const onDrop = (e: ReactDragEvent) => {
    e.preventDefault();
    const target = drop;
    setDrop(null);
    if (!target) return;
    const dragged = e.dataTransfer.getData("application/x-atlas-layer");
    const ids = selection.includes(dragged) ? selection : [dragged];
    const at = locate(page, target.id);
    if (!at || ids.includes(target.id)) return;
    if (target.where === "inside") return moveInto(ids, target.id);
    // The list is drawn front first: above a row is in front of it, a later child.
    const parentId = at.parent?.id ?? null;
    const siblings = (at.parent?.children ?? page.children).filter((n) => !ids.includes(n.id));
    const index = siblings.findIndex((n) => n.id === target.id);
    moveInto(ids, parentId, target.where === "above" ? index + 1 : index);
  };
  const onKey = (e: ReactKeyboardEvent) => {
    if (e.key === "F2" && selection.length === 1) {
      const row = document.querySelector<HTMLElement>(`.design-layer[aria-selected="true"]`);
      row?.dispatchEvent(new MouseEvent("dblclick", { bubbles: true }));
    }
  };
  return (
    <section className="design-section design-layers" aria-label={t("design.layers")}>
      <header className="design-section-head">
        <span>{t("design.layers")}</span>
      </header>
      {page.children.length === 0 ? (
        <p className="design-empty">{t("design.noLayers")}</p>
      ) : (
        <ul role="tree" aria-label={t("design.layers")} aria-multiselectable="true" onDrop={onDrop} onDragEnd={() => setDrop(null)} onKeyDown={onKey}>
          {page.children
            .slice()
            .reverse()
            .map((n) => (
              <LayerRow key={n.id} node={n} depth={0} drop={drop} setDrop={setDrop} collapsed={collapsed} toggle={toggle} />
            ))}
        </ul>
      )}
    </section>
  );
}

function Assets() {
  const doc = useDesign((s) => s.doc);
  const instance = useDesign((s) => s.instance);
  if (!doc) return null;
  const list = components(doc);
  return (
    <section className="design-section design-assets" aria-label={t("design.assets")}>
      <header className="design-section-head">
        <span>{t("design.components")}</span>
      </header>
      {list.length === 0 ? (
        <p className="design-empty">{t("design.noComponents")}</p>
      ) : (
        <ul>
          {list.map((c) => (
            <li key={c.id} className="design-asset">
              <TypeIcon type="component" />
              <span className="design-row-name">{c.name}</span>
              <button type="button" className="design-chip" onClick={() => instance(c.id)}>
                {t("design.insert")}
              </button>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

export function LeftPanel() {
  const [tab, setTab] = useState<"layers" | "assets" | "comments" | "history">("layers");
  const activeComment = useDesign((s) => s.activeComment);
  const open = (activeComment && tab !== "comments" ? "comments" : tab) as typeof tab;
  return (
    <aside className="design-left" aria-label={t("design.layers")}>
      <div className="design-tabs" role="tablist">
        <button type="button" role="tab" aria-selected={tab === "layers"} data-on={tab === "layers" || undefined} onClick={() => setTab("layers")}>
          {t("design.layers")}
        </button>
        <button type="button" role="tab" aria-selected={tab === "assets"} data-on={tab === "assets" || undefined} onClick={() => setTab("assets")}>
          {t("design.assets")}
        </button>
        <button type="button" role="tab" aria-selected={open === "comments"} data-on={open === "comments" || undefined} onClick={() => setTab("comments")}>
          {t("design.comments")}
        </button>
        <button type="button" role="tab" aria-selected={tab === "history"} data-on={tab === "history" || undefined} onClick={() => setTab("history")}>
          {t("design.history")}
        </button>
      </div>
      {open === "comments" ? (
        <CommentsPanel />
      ) : tab === "history" ? (
        <HistoryPanel />
      ) : tab === "layers" ? (
        <>
          <Pages />
          <LayerTree />
        </>
      ) : (
        <Assets />
      )}
    </aside>
  );
}
