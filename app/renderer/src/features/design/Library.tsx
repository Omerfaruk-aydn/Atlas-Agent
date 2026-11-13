// The design kit library: themes to switch the whole design to, ready-made
// blocks for web, iOS and Android, and the icon set. The kit itself lives in
// Go (internal/design/kit*.go); the agent uses the same themes and blocks
// through design_canvas, so what the person and the agent build matches.

import { useEffect, useMemo, useRef, useState } from "react";
import { http } from "../../api/http";
import { t } from "../../lib/i18n";
import { useApp } from "../../stores/app";
import { type KitOp, useDesign } from "../../stores/design";
import { zoomToFit } from "./Canvas";
import { type DesignNode, findNode, pageOf } from "./model";

export interface KitTheme {
  id: string;
  name: string;
  platform: "web" | "ios" | "android";
  note: string;
  source: string;
  dark_only?: boolean;
  fonts: { heading: string; body: string; mono: string };
  colors: Record<string, [string, string]>;
}

export interface KitBlock {
  id: string;
  platform: "web" | "ios" | "android";
  title: string;
  description: string;
  width: number;
  height: number;
}

export interface Kit {
  themes: KitTheme[];
  accents: { name: string; color: string }[];
  blocks: KitBlock[];
  icons: number;
}

export interface KitIcon {
  name: string;
  d: string;
  tags: string[] | null;
}

type Tab = "themes" | "blocks" | "icons";
export type Platform = KitBlock["platform"];
export const PLATFORMS: Platform[] = ["web", "ios", "android"];

const kitApi = (suffix: string) => `/api/workspaces/${encodeURIComponent(useApp.getState().workspace?.id ?? "")}/design/kit${suffix}`;

let kitCache: Promise<Kit> | null = null;
function loadKit(): Promise<Kit> {
  kitCache ??= http.get<Kit>(kitApi("")).catch((err) => {
    kitCache = null;
    throw err;
  });
  return kitCache;
}

/** Where a block goes: into the selected frame when it stacks its children in a column, else beside the page's content. */
export function blockTarget(selected: DesignNode | null): string | undefined {
  return selected && (selected.type === "frame" || selected.type === "component") && selected.layout?.dir === "column" ? selected.id : undefined;
}

/** The platform whose blocks to show first: the theme's, else web. */
export function platformOf(kit: Kit | null, themeId: string | undefined): Platform {
  return kit?.themes.find((th) => th.id === themeId)?.platform ?? "web";
}

function canvasView() {
  const el = document.querySelector<HTMLElement>(".design-canvas");
  return { width: el?.clientWidth ?? 800, height: el?.clientHeight ?? 600 };
}

function reveal() {
  const s = useDesign.getState();
  if (s.doc && s.selection.length) zoomToFit(pageOf(s.doc, s.pageId), canvasView(), true);
}

export function Library({ onClose }: { onClose: () => void }) {
  const doc = useDesign((s) => s.doc);
  const selection = useDesign((s) => s.selection);
  const [kit, setKit] = useState<Kit | null>(null);
  const [error, setError] = useState("");
  const [tab, setTab] = useState<Tab>(doc?.theme ? "blocks" : "themes");
  const [platform, setPlatform] = useState<Platform | null>(null);
  const [busy, setBusy] = useState(false);
  const panel = useRef<HTMLDivElement>(null);

  useEffect(() => {
    let live = true;
    loadKit()
      .then((k) => live && setKit(k))
      .catch((err) => live && setError(err instanceof Error ? err.message : String(err)));
    return () => {
      live = false;
    };
  }, []);
  useEffect(() => panel.current?.focus(), []);

  const theme = doc?.theme;
  const mode = doc?.variables?.mode === "Dark" ? "Dark" : "Light";
  const shown = platform ?? platformOf(kit, theme?.id);
  const selected = (doc && selection.length === 1 ? findNode(doc, selection[0]!)?.at.node : null) ?? null;

  const run = async (ops: KitOp[], show = false) => {
    if (busy) return;
    setBusy(true);
    try {
      const made = await useDesign.getState().applyKit(ops);
      if (show && made.length) requestAnimationFrame(reveal);
    } finally {
      setBusy(false);
    }
  };
  const setTheme = (id: string, accent?: string, nextMode?: string) => void run([{ op: "set_theme", name: id, ...(accent ? { accent } : {}), mode: nextMode ?? mode }]);
  const insert = (block: string) => void run([{ op: "insert", block, ...(blockTarget(selected) ? { parent: blockTarget(selected) } : {}) }], true);
  const addIcon = (name: string) => {
    const s = useDesign.getState();
    const holder = selected && (selected.type === "frame" || selected.type === "group" || selected.type === "component") ? selected : null;
    const view = canvasView();
    const at = holder ? {} : { x: Math.round((view.width / 2 - s.camera.x) / s.camera.zoom - 12), y: Math.round((view.height / 2 - s.camera.y) / s.camera.zoom - 12) };
    void run([{ op: "add", ...(holder ? { parent: holder.id } : {}), node: { type: "path", icon: name, width: 24, ...at } }]);
  };

  return (
    <div ref={panel} tabIndex={-1} className="design-library" role="dialog" aria-label={t("design.library")} aria-busy={busy} onKeyDown={(e) => e.key === "Escape" && (e.stopPropagation(), onClose())}>
      <div className="design-library-head">
        <h3>{t("design.library")}</h3>
        <button type="button" className="icon-button" aria-label={t("common.close")} onClick={onClose}>
          ×
        </button>
      </div>
      <div className="design-library-tabs" role="tablist" aria-label={t("design.library")}>
        {(["themes", "blocks", "icons"] as const).map((id) => (
          <button key={id} type="button" role="tab" aria-selected={tab === id} data-on={tab === id || undefined} className="design-chip" onClick={() => setTab(id)}>
            {t(`design.kit.${id}` as const)}
          </button>
        ))}
      </div>
      {error && <p className="design-hint" role="alert">{error}</p>}
      {!kit && !error && <div className="loading" role="status" aria-label={t("app.loading")} />}
      {kit && tab === "themes" && <Themes kit={kit} current={theme?.id} accent={theme?.accent} mode={mode} onTheme={setTheme} />}
      {kit && tab === "blocks" && (
        <Blocks kit={kit} platform={shown} onPlatform={setPlatform} themeId={theme?.id} accent={theme?.accent} mode={mode} target={blockTarget(selected) ? selected!.name : ""} onInsert={insert} />
      )}
      {tab === "icons" && <Icons total={kit?.icons ?? 0} into={selected && (selected.type === "frame" || selected.type === "group" || selected.type === "component") ? selected.name : ""} onPick={addIcon} />}
    </div>
  );
}

function Themes({ kit, current, accent, mode, onTheme }: { kit: Kit; current?: string; accent?: string; mode: string; onTheme: (id: string, accent?: string, mode?: string) => void }) {
  const active = kit.themes.find((th) => th.id === current);
  const m = mode === "Dark" ? 1 : 0;
  return (
    <div className="design-library-body">
      <p className="design-hint">{t("design.kit.themesHint")}</p>
      {active && (
        <>
          <div className="design-row design-library-modes" role="group" aria-label={t("design.kit.mode")}>
            {(["Light", "Dark"] as const).map((x) => (
              <button key={x} type="button" className="design-chip" data-on={mode === x || undefined} aria-pressed={mode === x} disabled={active.dark_only && x === "Light"} onClick={() => onTheme(active.id, accent, x)}>
                {t(x === "Light" ? "design.kit.light" : "design.kit.dark")}
              </button>
            ))}
          </div>
          <div className="design-library-accents" role="group" aria-label={t("design.kit.accent")}>
            <button type="button" className="design-accent" data-default data-on={!accent || undefined} aria-pressed={!accent} title={t("design.kit.themeAccent")} aria-label={t("design.kit.themeAccent")} style={{ background: active.colors.accent?.[m] }} onClick={() => onTheme(active.id, undefined)} />
            {kit.accents.map((a) => (
              <button key={a.name} type="button" className="design-accent" data-on={accent === a.name || undefined} aria-pressed={accent === a.name} title={a.name} aria-label={a.name} style={{ background: a.color }} onClick={() => onTheme(active.id, a.name)} />
            ))}
          </div>
        </>
      )}
      <ul className="design-library-themes">
        {kit.themes.map((th) => {
          const c = (k: string) => th.colors[k]?.[th.dark_only ? 1 : m] ?? "#000";
          return (
            <li key={th.id}>
              <button type="button" className="design-theme" data-on={th.id === current || undefined} aria-pressed={th.id === current} aria-label={th.name} title={`${th.note} (${th.source})`} onClick={() => onTheme(th.id, th.id === current ? accent : undefined)}>
                <span className="design-theme-swatch" style={{ background: c("bg"), color: c("text"), fontFamily: `"${th.fonts.heading}", "${th.fonts.heading} Variable", sans-serif` }}>
                  <span className="design-theme-aa">Aa</span>
                  <span className="design-theme-dots">
                    <i style={{ background: c("accent") }} />
                    <i style={{ background: c("card"), boxShadow: `inset 0 0 0 1px ${c("border")}` }} />
                    <i style={{ background: c("text-muted") }} />
                  </span>
                </span>
                <span className="design-theme-name">{th.name}</span>
                <span className="design-theme-meta">
                  {th.fonts.heading === th.fonts.body ? th.fonts.body : `${th.fonts.heading} · ${th.fonts.body}`} · {t(`design.kit.platform.${th.platform}`)}
                </span>
              </button>
            </li>
          );
        })}
      </ul>
    </div>
  );
}

function Blocks(props: { kit: Kit; platform: Platform; onPlatform: (p: Platform) => void; themeId?: string; accent?: string; mode: string; target: string; onInsert: (id: string) => void }) {
  const { kit, platform, onPlatform, themeId, accent, mode, target, onInsert } = props;
  const [q, setQ] = useState("");
  const needle = q.trim().toLowerCase();
  const list = kit.blocks.filter((b) => b.platform === platform && (!needle || `${b.title} ${b.id} ${b.description}`.toLowerCase().includes(needle)));
  // Previews draw in the design's theme when it is for this platform, else the platform's own.
  const themed = kit.themes.find((th) => th.id === themeId)?.platform === platform;
  const query = (block: string) => {
    const q = new URLSearchParams({ block });
    if (themed && themeId) {
      q.set("theme", themeId);
      if (accent) q.set("accent", accent);
      q.set("mode", mode);
    }
    return kitApi(`/preview?${q}`);
  };
  return (
    <div className="design-library-body">
      <div className="design-row" role="group" aria-label={t("design.kit.platform")}>
        {PLATFORMS.map((p) => (
          <button key={p} type="button" className="design-chip" data-on={platform === p || undefined} aria-pressed={platform === p} onClick={() => onPlatform(p)}>
            {t(`design.kit.platform.${p}`)}
          </button>
        ))}
      </div>
      <p className="design-hint">{target ? t("design.kit.insertInto").replace("{name}", target) : t("design.kit.insertHint")}</p>
      <input className="design-input" value={q} onChange={(e) => setQ(e.target.value)} placeholder={t("design.kit.search")} aria-label={t("design.kit.search")} />
      <ul className="design-library-blocks">
        {list.length === 0 && <li className="design-hint">{t("design.kit.noBlocks")}</li>}
        {list.map((b) => (
          <li key={b.id}>
            <button type="button" className="design-block" aria-label={b.title} title={b.description} onClick={() => onInsert(b.id)}>
              <span className="design-block-preview" style={{ aspectRatio: `${Math.max(1, b.width)} / ${Math.min(Math.max(1, b.height), Math.max(1, b.width) * 1.6)}` }}>
                <img src={query(b.id)} alt="" loading="lazy" draggable={false} />
              </span>
              <span className="design-block-title">{b.title}</span>
              <span className="design-block-desc">{b.description}</span>
            </button>
          </li>
        ))}
      </ul>
    </div>
  );
}

function Icons({ total, into, onPick }: { total: number; into: string; onPick: (name: string) => void }) {
  const [q, setQ] = useState("");
  const [icons, setIcons] = useState<KitIcon[]>([]);
  const input = useRef<HTMLInputElement>(null);
  useEffect(() => input.current?.focus(), []);
  useEffect(() => {
    let live = true;
    const timer = setTimeout(() => {
      http
        .get<{ icons: KitIcon[] | null }>(kitApi(`/icons${q.trim() ? `?q=${encodeURIComponent(q.trim())}` : ""}`))
        .then((res) => live && setIcons(res.icons ?? []))
        .catch(() => live && setIcons([]));
    }, 150);
    return () => {
      live = false;
      clearTimeout(timer);
    };
  }, [q]);
  const hint = useMemo(() => (into ? t("design.kit.iconInto").replace("{name}", into) : t("design.kit.iconHint").replace("{count}", total.toLocaleString())), [into, total]);
  return (
    <div className="design-library-body">
      <input ref={input} className="design-library-search" type="search" value={q} placeholder={t("design.kit.searchIcons")} aria-label={t("design.kit.searchIcons")} onChange={(e) => setQ(e.target.value)} />
      <p className="design-hint">{hint}</p>
      {icons.length === 0 && q.trim() && <p className="design-hint">{t("design.kit.noIcons")}</p>}
      <ul className="design-library-icons">
        {icons.map((ic) => (
          <li key={ic.name}>
            <button type="button" className="design-icon" title={ic.name} aria-label={ic.name} onClick={() => onPick(ic.name)}>
              <svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                <path d={ic.d} />
              </svg>
            </button>
          </li>
        ))}
      </ul>
    </div>
  );
}
