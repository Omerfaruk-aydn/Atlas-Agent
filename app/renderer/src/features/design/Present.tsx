// The presentation: clickable frames. Interactions move between frames and
// overlays with transitions, scroll areas scroll, and "smart animate" glides
// layers that exist in both frames.

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { t } from "../../lib/i18n";
import { useDesign } from "../../stores/design";
import { imageUrl } from "./Canvas";
import { type DesignNode, type Interaction, pageOf } from "./model";
import { ambient, chainTo, EASING, initial, interactionFor, matchByName, type ProtoState, step } from "./proto";
import { NodeView, RenderContext, type RenderEnv } from "./render";
import { resolveTokens } from "./tokens";
import { components } from "./model";
import { sizeOf } from "./text";

interface Running {
  from: string;
  to: string;
  interaction: Interaction;
  /** Changes with every move, so the stage restarts its animation. */
  id: number;
}

const DEFAULT_MS = 300;

/** How far a scroll area's content reaches past its box. */
function reach(n: DesignNode): { x: number; y: number } {
  let right = n.width;
  let bottom = n.height;
  for (const c of n.children ?? []) {
    const s = sizeOf(c);
    right = Math.max(right, c.x + s.width);
    bottom = Math.max(bottom, c.y + s.height);
  }
  return { x: Math.max(0, right - n.width), y: Math.max(0, bottom - n.height) };
}

export function Present({ onClose }: { onClose: () => void }) {
  const doc = useDesign((s) => s.doc)!;
  const pageId = useDesign((s) => s.pageId);
  const selection = useDesign((s) => s.selection);
  const resolved = useMemo(() => resolveTokens(doc), [doc]);
  const page = pageOf(resolved, pageId);
  const [state, setState] = useState<ProtoState | null>(() => initial(page, selection));
  const [running, setRunning] = useState<Running | null>(null);
  const [scroll, setScroll] = useState<Record<string, { x: number; y: number }>>({});
  const [box, setBox] = useState({ width: 0, height: 0 });
  const stage = useRef<HTMLDivElement>(null);
  const layers = useRef(new Map<string, SVGSVGElement>());
  const counter = useRef(0);
  const hovered = useRef<string | null>(null);

  const env = useMemo<RenderEnv>(() => ({ prefix: "p", components: new Map(components(resolved).map((c) => [c.id, c])), image: imageUrl, scroll }), [resolved, scroll]);

  useEffect(() => {
    const el = stage.current;
    if (!el) return;
    const ro = new ResizeObserver(() => setBox({ width: el.clientWidth, height: el.clientHeight }));
    ro.observe(el);
    setBox({ width: el.clientWidth, height: el.clientHeight });
    return () => ro.disconnect();
  }, []);

  const frameOf = useCallback((id: string) => page.children.find((c) => c.id === id), [page]);
  const current = state ? frameOf(state.stack[state.stack.length - 1]!) : undefined;

  /** Moves the presentation and animates the change. */
  const go = useCallback(
    (it: Interaction) => {
      if (!state) return;
      const out = step(page, state, it);
      if (!out) return;
      if (out.url) {
        window.open(out.url, "_blank", "noopener,noreferrer");
        return;
      }
      if (out.scroll) {
        const chain = chainTo(page, out.scroll);
        const holder = [...chain].reverse().find((n, i) => i > 0 && n.scroll);
        const target = chain[chain.length - 1];
        if (holder && target) setScroll((s) => ({ ...s, [holder.id]: { x: s[holder.id]?.x ?? 0, y: Math.min(reach(holder).y, Math.max(0, target.y)) } }));
        return;
      }
      const from = state.stack[state.stack.length - 1]!;
      const to = out.state.stack[out.state.stack.length - 1]!;
      setState(out.state);
      if (from !== to || out.state.overlays.length !== state.overlays.length) setRunning({ from, to, interaction: it, id: ++counter.current });
    },
    [page, state],
  );

  // After-delay interactions start when a frame or overlay appears; keys act while it is shown.
  useEffect(() => {
    if (!state) return;
    const timers = ambient(page, state, "after").map(({ interaction }) => window.setTimeout(() => go(interaction), interaction.delay ?? 0));
    return () => timers.forEach(window.clearTimeout);
  }, [state, page, go]);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.preventDefault();
        e.stopPropagation();
        onClose();
        return;
      }
      if (!state) return;
      const hit = ambient(page, state, "key", e.key)[0];
      if (hit) {
        e.preventDefault();
        e.stopPropagation();
        go(hit.interaction);
      }
    };
    window.addEventListener("keydown", onKey, true);
    return () => window.removeEventListener("keydown", onKey, true);
  }, [state, page, go, onClose]);

  /** The node ids from the element under the pointer outwards. */
  const chainAt = (target: EventTarget | null): DesignNode[] => {
    let el = target as Element | null;
    while (el && !el.getAttribute?.("data-node")) el = el.parentElement;
    const id = el?.getAttribute("data-node");
    return id ? chainTo(page, id) : [];
  };

  const trigger = (e: React.SyntheticEvent, kind: "click" | "press") => {
    const hit = interactionFor(chainAt(e.target), kind);
    if (hit) go(hit.interaction);
  };

  const onOver = (e: React.PointerEvent) => {
    const chain = chainAt(e.target);
    const hit = interactionFor(chain, "hover");
    const id = hit?.node.id ?? null;
    if (id !== hovered.current) {
      hovered.current = id;
      if (hit) go(hit.interaction);
    }
  };

  const dragStart = useRef<{ x: number; y: number; chain: DesignNode[] } | null>(null);

  const onWheel = (e: React.WheelEvent) => {
    // The innermost scroll area under the pointer that has somewhere to go.
    const chain = chainAt(e.target);
    for (let i = chain.length - 1; i >= 0; i--) {
      const n = chain[i]!;
      if (!n.scroll) continue;
      const max = reach(n);
      const cur = scroll[n.id] ?? { x: 0, y: 0 };
      const x = n.scroll === "vertical" ? 0 : Math.min(max.x, Math.max(0, cur.x + e.deltaX));
      const y = n.scroll === "horizontal" ? 0 : Math.min(max.y, Math.max(0, cur.y + e.deltaY));
      if (x !== cur.x || y !== cur.y) {
        setScroll((s) => ({ ...s, [n.id]: { x, y } }));
        return;
      }
    }
  };

  // Plays the transition between the frames now on stage.
  useEffect(() => {
    if (!running) return;
    const { from, to, interaction } = running;
    const oldLayer = layers.current.get(from);
    const newLayer = layers.current.get(to);
    const duration = interaction.duration ?? DEFAULT_MS;
    const easing = EASING[interaction.easing ?? "ease_out"];
    const anims: Animation[] = [];
    const dir = interaction.direction ?? "left";
    const dx = dir === "left" ? 1 : dir === "right" ? -1 : 0;
    const dy = dir === "up" ? 1 : dir === "down" ? -1 : 0;
    const overlay = state?.overlays.some((o) => o.id === to);
    if (newLayer && duration > 0 && interaction.transition !== "instant") {
      const w = newLayer.clientWidth;
      const h = newLayer.clientHeight;
      const opts: KeyframeAnimationOptions = { duration, easing, fill: "both" };
      switch (interaction.transition ?? "dissolve") {
        case "slide":
        case "move":
          anims.push(newLayer.animate([{ transform: `translate(${dx * w}px, ${dy * h}px)` }, { transform: "translate(0, 0)" }], opts));
          break;
        case "push":
          anims.push(newLayer.animate([{ transform: `translate(${dx * w}px, ${dy * h}px)` }, { transform: "translate(0, 0)" }], opts));
          if (oldLayer) anims.push(oldLayer.animate([{ transform: "translate(0, 0)" }, { transform: `translate(${-dx * w}px, ${-dy * h}px)` }], opts));
          break;
        case "smart": {
          anims.push(newLayer.animate([{ opacity: 0 }, { opacity: 1 }], { ...opts, duration: duration * 0.6 }));
          const a = frameOf(from);
          const b = frameOf(to);
          if (a && b) {
            for (const [oldId, next] of matchByName(a, b)) {
              const old = chainTo(page, oldId).slice(1).reduce((acc, n) => ({ x: acc.x + n.x, y: acc.y + n.y, opacity: acc.opacity * (n.opacity ?? 1) }), { x: 0, y: 0, opacity: 1 });
              const now = chainTo(page, next.id).slice(1).reduce((acc, n) => ({ x: acc.x + n.x, y: acc.y + n.y, opacity: acc.opacity * (n.opacity ?? 1) }), { x: 0, y: 0, opacity: 1 });
              const el = newLayer.querySelector<SVGGElement>(`[data-node="${CSS.escape(next.id)}"]`);
              const parent = el?.parentElement?.closest("[data-node]");
              // Only layers directly in the frame are tweened, so their own offset is the page offset.
              if (el && (!parent || parent.getAttribute("data-node") === to)) {
                anims.push(el.animate([{ transform: `translate(${old.x}px, ${old.y}px)`, opacity: old.opacity }, { transform: `translate(${now.x}px, ${now.y}px)`, opacity: now.opacity }], opts));
              }
            }
          }
          if (oldLayer) anims.push(oldLayer.animate([{ opacity: 1 }, { opacity: 0 }], opts));
          break;
        }
        default:
          anims.push(newLayer.animate([{ opacity: overlay ? 0 : 0 }, { opacity: 1 }], opts));
      }
    }
    let live = true;
    const done = Promise.all(anims.map((a) => a.finished.catch(() => undefined))).then(() => {
      if (live) setRunning((r) => (r && r.id === running.id ? null : r));
    });
    void done;
    return () => {
      live = false;
      anims.forEach((a) => a.cancel());
    };
  }, [running]); // eslint-disable-line react-hooks/exhaustive-deps

  if (!state || !current) {
    return (
      <div className="design-present" role="dialog" aria-label={t("design.present")}>
        <p className="design-muted">{t("design.presentEmpty")}</p>
        <button type="button" className="design-chip" onClick={onClose}>
          {t("design.exitPresent")}
        </button>
      </div>
    );
  }

  const scale = Math.max(0.05, Math.min((box.width - 32) / Math.max(1, current.width), (box.height - 32) / Math.max(1, current.height)));
  const shown = [state.stack[state.stack.length - 1]!, ...state.overlays.map((o) => o.id)];
  // The frame being left stays underneath until the transition ends.
  const under = running && !shown.includes(running.from) ? frameOf(running.from) : undefined;

  const layer = (f: DesignNode, overlay?: { position: string }) => {
    const left = overlay ? (overlay.position === "left" ? 0 : overlay.position === "right" ? current.width - f.width : (current.width - f.width) / 2) : 0;
    const top = overlay ? (overlay.position === "top" ? 0 : overlay.position === "bottom" ? current.height - f.height : (current.height - f.height) / 2) : 0;
    return (
      <svg
        key={f.id}
        ref={(el) => {
          if (el) layers.current.set(f.id, el);
          else layers.current.delete(f.id);
        }}
        className="design-present-frame"
        width={f.width * scale}
        height={f.height * scale}
        viewBox={`0 0 ${f.width} ${f.height}`}
        style={{ left: left * scale, top: top * scale }}
        aria-label={f.name}
      >
        <g transform={`translate(${-f.x} ${-f.y})`}>
          <NodeView node={f} />
        </g>
      </svg>
    );
  };

  return (
    <div className="design-present" role="dialog" aria-label={t("design.present")} data-ready="">
      <div className="design-present-bar">
        <button type="button" className="design-chip" onClick={() => go({ trigger: "click", action: "back" })} disabled={state.stack.length < 2 && state.overlays.length === 0} aria-label={t("design.back")}>
          ←
        </button>
        <button type="button" className="design-chip" onClick={() => setState(initial(page, selection))}>
          {t("design.restart")}
        </button>
        <span className="design-muted">{current.name}</span>
        <button type="button" className="design-chip" onClick={onClose} aria-label={t("design.exitPresent")}>
          {t("design.exitPresent")}
        </button>
      </div>
      <div
        ref={stage}
        className="design-present-stage"
        onClick={(e) => trigger(e, "click")}
        onPointerDown={(e) => {
          trigger(e, "press");
          dragStart.current = { x: e.clientX, y: e.clientY, chain: chainAt(e.target) };
        }}
        onPointerMove={(e) => {
          onOver(e);
          const d = dragStart.current;
          if (d && Math.hypot(e.clientX - d.x, e.clientY - d.y) > 24) {
            dragStart.current = null;
            const hit = interactionFor(d.chain, "drag");
            if (hit) go(hit.interaction);
          }
        }}
        onPointerUp={() => (dragStart.current = null)}
        onWheel={onWheel}
      >
        <RenderContext.Provider value={env}>
          <div className="design-present-canvas" style={{ width: current.width * scale, height: current.height * scale }}>
            {under && layer(under)}
            {shown.map((id, i) => {
              const f = frameOf(id);
              return f ? layer(f, i > 0 ? state.overlays[i - 1] : undefined) : null;
            })}
          </div>
        </RenderContext.Provider>
      </div>
    </div>
  );
}
