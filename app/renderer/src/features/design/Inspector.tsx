// The right side of the design tool: the selection's properties. Fields
// show a value when every selected node shares it, and "Mixed" otherwise;
// a change applies to all of them as one undo step.

import { useEffect, useRef, useState, type ReactNode } from "react";
import { t } from "../../lib/i18n";
import { useDesign } from "../../stores/design";
import { type AlignKind } from "./geometry";
import { components, type DesignDoc, type DesignNode, type Effect, type Interaction, isColor, locate, newId, type Paint, pageOf, splitColor, type Stroke } from "./model";
import { masterFor } from "./tokens";
import { type CodeKind, codeFor } from "./inspect";
import { IconAlign, IconDistH, IconDistV } from "./icons";
import { arithmetic } from "./arith";
import { sizeOf } from "./text";
import { runExport } from "./exporter";
import type { ExportFormat } from "./export";

const MIXED = Symbol("mixed");
type Value<T> = T | typeof MIXED;

function shared<T>(nodes: DesignNode[], get: (n: DesignNode) => T): Value<T> {
  if (nodes.length === 0) return MIXED;
  const first = get(nodes[0]!);
  const same = nodes.every((n) => JSON.stringify(get(n)) === JSON.stringify(first));
  return same ? first : MIXED;
}

const round = (v: number) => Math.round(v * 100) / 100;


/** A number field: type and press Enter, arrow keys step (shift ×10), drag the label to scrub. */
export function NumberField({ label, value, onChange, min, max, step = 1, suffix, title }: { label: string; value: Value<number>; onChange: (v: number) => void; min?: number; max?: number; step?: number; suffix?: string; title?: string }) {
  const [text, setText] = useState(value === MIXED ? "" : String(round(value)));
  const focused = useRef(false);
  const cancelled = useRef(false);
  useEffect(() => {
    if (!focused.current) setText(value === MIXED ? "" : String(round(value)));
  }, [value]);
  const clamp = (v: number) => Math.min(max ?? Infinity, Math.max(min ?? -Infinity, v));
  const commit = () => {
    const expr = text.trim();
    if (!expr) return;
    // Simple arithmetic, as design tools allow: "100/2", "24+8".
    const n = arithmetic(expr);
    if (Number.isFinite(n)) onChange(clamp(n));
    else setText(value === MIXED ? "" : String(round(value)));
  };
  const scrub = useRef<{ x: number; start: number } | null>(null);
  return (
    <label className="design-field" title={title ?? label}>
      <span
        className="design-field-label"
        onPointerDown={(e) => {
          if (value === MIXED) return;
          e.currentTarget.setPointerCapture(e.pointerId);
          scrub.current = { x: e.clientX, start: value };
          useDesign.getState().hold(true);
        }}
        onPointerMove={(e) => {
          if (!scrub.current) return;
          const v = clamp(round(scrub.current.start + Math.round((e.clientX - scrub.current.x) / 2) * step * (e.shiftKey ? 10 : 1)));
          setText(String(v));
          onChange(v);
        }}
        onPointerUp={() => {
          if (!scrub.current) return;
          scrub.current = null;
          useDesign.getState().hold(false);
        }}
      >
        {label}
      </span>
      <input
        type="text"
        inputMode="decimal"
        aria-label={title ?? label}
        value={text}
        placeholder={value === MIXED ? t("design.mixed") : undefined}
        onFocus={(e) => {
          focused.current = true;
          e.currentTarget.select();
        }}
        onBlur={() => {
          focused.current = false;
          if (cancelled.current) cancelled.current = false;
          else commit();
        }}
        onChange={(e) => setText(e.target.value)}
        onKeyDown={(e) => {
          e.stopPropagation();
          if (e.key === "Enter") {
            // Blurring commits.
            e.currentTarget.blur();
          } else if (e.key === "Escape") {
            cancelled.current = true;
            setText(value === MIXED ? "" : String(round(value)));
            e.currentTarget.blur();
          } else if ((e.key === "ArrowUp" || e.key === "ArrowDown") && value !== MIXED) {
            e.preventDefault();
            const v = clamp(round(value + (e.key === "ArrowUp" ? 1 : -1) * step * (e.shiftKey ? 10 : 1)));
            setText(String(v));
            onChange(v);
          }
        }}
      />
      {suffix && <span className="design-field-suffix">{suffix}</span>}
    </label>
  );
}

/** A colour: the swatch opens the system picker; the hex can be typed. */
export function ColorField({ label, value, onChange }: { label: string; value: string; onChange: (hex: string) => void }) {
  const { rgb, alpha } = splitColor(value);
  const [hex, setHex] = useState(rgb.slice(1));
  useEffect(() => setHex(rgb.slice(1)), [rgb]);
  const withAlpha = (six: string, a: number) => (a >= 1 ? `#${six}` : `#${six}${Math.round(a * 255).toString(16).padStart(2, "0")}`).toUpperCase();
  return (
    <div className="design-color">
      <input type="color" aria-label={label} value={rgb.toLowerCase()} onChange={(e) => onChange(withAlpha(e.target.value.slice(1), alpha))} />
      <input
        type="text"
        className="design-hex"
        aria-label={`${label} hex`}
        value={hex}
        maxLength={8}
        onChange={(e) => setHex(e.target.value.replace(/[^0-9a-f]/gi, ""))}
        onBlur={() => {
          const v = `#${hex}`;
          if (isColor(v) && (hex.length === 3 || hex.length === 6 || hex.length === 8)) onChange(hex.length === 8 ? v.toUpperCase() : withAlpha(splitColor(v).rgb.slice(1), alpha));
          else setHex(rgb.slice(1));
        }}
        onKeyDown={(e) => {
          e.stopPropagation();
          if (e.key === "Enter") e.currentTarget.blur();
        }}
      />
      <NumberField label="%" title={`${label} ${t("design.opacity")}`} value={Math.round(alpha * 100)} min={0} max={100} onChange={(v) => onChange(withAlpha(rgb.slice(1), v / 100))} />
    </div>
  );
}

function Section({ title, children, action }: { title: string; children: ReactNode; action?: ReactNode }) {
  return (
    <section className="design-section">
      <header className="design-section-head">
        <span>{title}</span>
        {action}
      </header>
      <div className="design-section-body">{children}</div>
    </section>
  );
}

const AddButton = ({ label, onClick }: { label: string; onClick: () => void }) => (
  <button type="button" className="icon-button design-mini" aria-label={label} title={label} onClick={onClick}>
    +
  </button>
);

function paintColor(p: Paint): string {
  return p.type === "solid" ? p.color : p.type === "image" ? "#D9D9D9" : (p.stops[0]?.color ?? "#000000");
}

const MAX_FILL_IMAGE = 12 * 1024 * 1024;

/** Asks for an image file and reads it as a data URL; null when none was chosen. */
function pickImage(): Promise<string | null> {
  return new Promise((resolve) => {
    const input = document.createElement("input");
    input.type = "file";
    input.accept = "image/png,image/jpeg,image/gif,image/webp,image/svg+xml";
    input.onchange = () => {
      const file = input.files?.[0];
      if (!file || file.size > MAX_FILL_IMAGE) return resolve(null);
      const reader = new FileReader();
      reader.onload = () => resolve(String(reader.result));
      reader.onerror = () => resolve(null);
      reader.readAsDataURL(file);
    };
    input.addEventListener("cancel", () => resolve(null));
    input.click();
  });
}

/** What a style puts on a node, so the stored node matches what is drawn. */
function copyStyle(doc: DesignDoc, kind: "fill" | "text" | "effect", id: string): Partial<DesignNode> {
  if (!id) return {};
  if (kind === "fill") return { fills: doc.styles?.fills?.[id]?.fills };
  if (kind === "effect") return { effects: doc.styles?.effects?.[id]?.effects };
  const { name: _name, ...rest } = doc.styles?.text?.[id] ?? ({} as never);
  return rest as Partial<DesignNode>;
}

/** Picks, creates and clears a shared style of one kind for the selected nodes. */
function StyleBar({ kind, nodes, patch }: { kind: "fill" | "text" | "effect"; nodes: DesignNode[]; patch: Patcher }) {
  const doc = useDesign((s) => s.doc)!;
  const commit = useDesign((s) => s.commit);
  const key = kind === "fill" ? "fills" : kind === "text" ? "text" : "effects";
  const ref = `${kind}_style` as "fill_style" | "text_style" | "effect_style";
  const styles = (doc.styles?.[key] ?? {}) as Record<string, { name: string }>;
  const ids = Object.keys(styles);
  const current = shared(nodes, (n) => n[ref] ?? "");
  const first = nodes[0]!;
  const create = () => {
    const id = newId();
    const name = `${t(`design.style.${kind}` as never)} ${ids.length + 1}`;
    const entry = kind === "fill" ? { name, fills: first.fills ?? [] } : kind === "effect" ? { name, effects: first.effects ?? [] } : { name, font: first.font!, line_height: first.line_height, letter_spacing: first.letter_spacing, align: first.align, decoration: first.decoration, transform: first.transform };
    commit({ ...doc, styles: { ...doc.styles, [key]: { ...(doc.styles?.[key] ?? {}), [id]: entry } } } as DesignDoc);
    patch(() => ({ [ref]: id }));
  };
  if (ids.length === 0 && (kind === "fill" ? !first.fills?.length : kind === "effect" ? !first.effects?.length : false)) return null;
  return (
    <div className="design-row">
      <select aria-label={t(`design.style.${kind}` as never)} className="design-grow" value={current === MIXED ? "" : current} onChange={(e) => patch(() => ({ [ref]: e.target.value || undefined, ...copyStyle(doc, kind, e.target.value) }))}>
        <option value="">{current === MIXED ? t("design.mixed") : t("design.noStyle")}</option>
        {ids.map((id) => (
          <option key={id} value={id}>
            {styles[id]!.name}
          </option>
        ))}
      </select>
      <button type="button" className="design-row-tool" aria-label={t("design.createStyle")} title={t("design.createStyle")} disabled={nodes.length !== 1} onClick={create}>
        +
      </button>
    </div>
  );
}

/** The colour variable a solid fill follows, if any. */
function VariablePick({ value, onChange }: { value: string | undefined; onChange: (id: string | undefined) => void }) {
  const doc = useDesign((s) => s.doc)!;
  const items = Object.entries(doc.variables?.items ?? {});
  if (items.length === 0) return null;
  return (
    <select aria-label={t("design.variable")} title={t("design.variable")} value={value ?? ""} onChange={(e) => onChange(e.target.value || undefined)}>
      <option value="">{t("design.noVariable")}</option>
      {items.map(([id, v]) => (
        <option key={id} value={id}>
          {v.name}
        </option>
      ))}
    </select>
  );
}

function Fills({ nodes, patch: rawPatch }: { nodes: DesignNode[]; patch: Patcher }) {
  const doc = useDesign((s) => s.doc)!;
  // Editing a fill by hand leaves the shared style, as the node now looks its own way.
  const patch: Patcher = (fn) => rawPatch((n) => ({ fill_style: undefined, ...fn(n) }));
  const variableColor = (id: string | undefined, fallback: string) => (id && doc.variables ? (doc.variables.items[id]?.values[doc.variables.mode] ?? fallback) : fallback);
  const fills = shared(nodes, (n) => n.fills ?? []);
  return (
    <Section title={t("design.fill")} action={<AddButton label={t("design.addFill")} onClick={() => patch((n) => ({ fills: [...(n.fills ?? []), { type: "solid", color: n.type === "text" ? "#000000" : "#D9D9D9" }] }))} />}>
      <StyleBar kind="fill" nodes={nodes} patch={rawPatch} />
      {fills === MIXED ? (
        <p className="design-muted">{t("design.mixed")}</p>
      ) : (
        fills.map((f, i) => (
          <div className="design-row" key={i}>
            <select
              aria-label={t("design.fillType")}
              value={f.type}
              onChange={(e) => {
                const type = e.target.value as Paint["type"];
                if (type === "image") {
                  void pickImage().then((src) => src && patch((n) => ({ fills: (n.fills ?? []).map((x, k): Paint => (k === i ? { type: "image", src } : x)) })));
                  return;
                }
                patch((n) => ({
                  fills: (n.fills ?? []).map((x, k): Paint => {
                    if (k !== i) return x;
                    const c = paintColor(x);
                    return type === "solid" ? { type, color: c } : { type: type as "linear" | "radial", stops: [{ offset: 0, color: c }, { offset: 1, color: "#FFFFFF" }], angle: 90 };
                  }),
                }));
              }}
            >
              <option value="solid">{t("design.solid")}</option>
              <option value="linear">{t("design.linear")}</option>
              <option value="radial">{t("design.radial")}</option>
              <option value="image">{t("design.imageFill")}</option>
            </select>
            {f.type === "solid" ? (
              <>
                <ColorField label={t("design.fill")} value={f.color} onChange={(c) => patch((n) => ({ fills: (n.fills ?? []).map((x, k) => (k === i ? { ...x, color: c, variable: undefined } : x)) as Paint[] }))} />
                <VariablePick value={f.variable} onChange={(id) => patch((n) => ({ fills: (n.fills ?? []).map((x, k) => (k === i && x.type === "solid" ? { ...x, variable: id, color: variableColor(id, x.color) } : x)) as Paint[] }))} />
              </>
            ) : f.type === "image" ? (
              <div className="design-stops">
                <select aria-label={t("design.imageFit")} value={f.fit ?? "cover"} onChange={(e) => patch((n) => ({ fills: (n.fills ?? []).map((x, k) => (k === i && x.type === "image" ? { ...x, fit: e.target.value as "cover" | "contain" | "stretch" } : x)) }))}>
                  <option value="cover">{t("design.fitCover")}</option>
                  <option value="contain">{t("design.fitContain")}</option>
                  <option value="stretch">{t("design.fitStretch")}</option>
                </select>
                <button type="button" className="design-chip" onClick={() => void pickImage().then((src) => src && patch((n) => ({ fills: (n.fills ?? []).map((x, k) => (k === i && x.type === "image" ? { ...x, src } : x)) })))}>
                  {t("design.replaceImage")}
                </button>
              </div>
            ) : (
              <div className="design-stops">
                {f.stops.map((s, k) => (
                  <ColorField
                    key={k}
                    label={`${t("design.stop")} ${k + 1}`}
                    value={s.color}
                    onChange={(c) => patch((n) => ({ fills: (n.fills ?? []).map((x, j) => (j === i && (x.type === "linear" || x.type === "radial") ? { ...x, stops: x.stops.map((st, q) => (q === k ? { ...st, color: c } : st)) } : x)) }))}
                  />
                ))}
                {f.type === "linear" && <NumberField label="∠" title={t("design.angle")} value={f.angle ?? 90} onChange={(v) => patch((n) => ({ fills: (n.fills ?? []).map((x, j) => (j === i && x.type === "linear" ? { ...x, angle: v } : x)) }))} />}
              </div>
            )}
            <button type="button" className="design-row-tool" aria-label={`${t("design.remove")} ${t("design.fill")}`} onClick={() => patch((n) => ({ fills: (n.fills ?? []).filter((_, k) => k !== i) }))}>
              −
            </button>
          </div>
        ))
      )}
    </Section>
  );
}

function Strokes({ nodes, patch }: { nodes: DesignNode[]; patch: (fn: (n: DesignNode) => Partial<DesignNode>) => void }) {
  const strokes = shared(nodes, (n) => n.strokes ?? []);
  const edit = (i: number, change: Partial<Stroke>) => patch((n) => ({ strokes: (n.strokes ?? []).map((x, k) => (k === i ? { ...x, ...change } : x)) }));
  return (
    <Section title={t("design.stroke")} action={<AddButton label={t("design.addStroke")} onClick={() => patch((n) => ({ strokes: [...(n.strokes ?? []), { color: "#000000", width: 1, align: n.type === "line" ? "center" : "inside" }] }))} />}>
      {strokes === MIXED ? (
        <p className="design-muted">{t("design.mixed")}</p>
      ) : (
        strokes.map((s, i) => (
          <div className="design-row design-wrap" key={i}>
            <ColorField label={t("design.stroke")} value={s.color} onChange={(c) => edit(i, { color: c })} />
            <NumberField label="W" title={t("design.strokeWidth")} value={s.width} min={0} onChange={(v) => edit(i, { width: v })} />
            <select aria-label={t("design.strokeAlign")} value={s.align ?? "center"} onChange={(e) => edit(i, { align: e.target.value as Stroke["align"] })}>
              <option value="inside">{t("design.inside")}</option>
              <option value="center">{t("design.center")}</option>
              <option value="outside">{t("design.outside")}</option>
            </select>
            <label className="design-check">
              <input type="checkbox" checked={!!s.dash?.length} onChange={(e) => edit(i, { dash: e.target.checked ? [4, 4] : [] })} />
              {t("design.dashed")}
            </label>
            <button type="button" className="design-row-tool" aria-label={`${t("design.remove")} ${t("design.stroke")}`} onClick={() => patch((n) => ({ strokes: (n.strokes ?? []).filter((_, k) => k !== i) }))}>
              −
            </button>
          </div>
        ))
      )}
    </Section>
  );
}

function Effects({ nodes, patch }: { nodes: DesignNode[]; patch: (fn: (n: DesignNode) => Partial<DesignNode>) => void }) {
  const effects = shared(nodes, (n) => n.effects ?? []);
  const edit = (i: number, change: Partial<Effect>) => patch((n) => ({ effects: (n.effects ?? []).map((x, k) => (k === i ? { ...x, ...change } : x)) }));
  return (
    <Section title={t("design.effects")} action={<AddButton label={t("design.addEffect")} onClick={() => patch((n) => ({ effects: [...(n.effects ?? []), { type: "drop-shadow", x: 0, y: 4, blur: 12, spread: 0, color: "#00000026" }], effect_style: undefined }))} />}>
      <StyleBar kind="effect" nodes={nodes} patch={patch} />
      {effects === MIXED ? (
        <p className="design-muted">{t("design.mixed")}</p>
      ) : (
        effects.map((e, i) => (
          <div className="design-row design-wrap" key={i}>
            <select aria-label={t("design.effectType")} value={e.type} onChange={(ev) => edit(i, { type: ev.target.value as Effect["type"], ...(ev.target.value !== "blur" && e.color === undefined ? { x: 0, y: 4, spread: 0, color: "#00000026" } : {}) })}>
              <option value="drop-shadow">{t("design.dropShadow")}</option>
              <option value="inner-shadow">{t("design.innerShadow")}</option>
              <option value="blur">{t("design.layerBlur")}</option>
            </select>
            {e.type !== "blur" && (
              <>
                <NumberField label="X" value={e.x ?? 0} onChange={(v) => edit(i, { x: v })} />
                <NumberField label="Y" value={e.y ?? 0} onChange={(v) => edit(i, { y: v })} />
              </>
            )}
            <NumberField label="B" title={t("design.blur")} value={e.blur} min={0} onChange={(v) => edit(i, { blur: v })} />
            {e.type !== "blur" && (
              <>
                <NumberField label="S" title={t("design.spread")} value={e.spread ?? 0} onChange={(v) => edit(i, { spread: v })} />
                <ColorField label={t("design.shadowColor")} value={e.color ?? "#00000040"} onChange={(c) => edit(i, { color: c })} />
              </>
            )}
            <button type="button" className="design-row-tool" aria-label={`${t("design.remove")} ${t("design.effects")}`} onClick={() => patch((n) => ({ effects: (n.effects ?? []).filter((_, k) => k !== i) }))}>
              −
            </button>
          </div>
        ))
      )}
    </Section>
  );
}

const FONTS = ["Inter", "JetBrains Mono", "system-ui", "Arial", "Helvetica", "Segoe UI", "Roboto", "Georgia", "Times New Roman", "Courier New", "Verdana", "Trebuchet MS"];
const WEIGHTS = [100, 200, 300, 400, 500, 600, 700, 800, 900];

function TextProps({ nodes, patch }: { nodes: DesignNode[]; patch: (fn: (n: DesignNode) => Partial<DesignNode>) => void }) {
  const family = shared(nodes, (n) => n.font?.family ?? "Inter");
  const size = shared(nodes, (n) => n.font?.size ?? 16);
  const weight = shared(nodes, (n) => n.font?.weight ?? 400);
  const lh = shared(nodes, (n) => n.line_height ?? 0);
  const ls = shared(nodes, (n) => n.letter_spacing ?? 0);
  const align = shared(nodes, (n) => n.align ?? "left");
  const auto = shared(nodes, (n) => n.auto_resize ?? "width");
  const font = (change: Partial<NonNullable<DesignNode["font"]>>) => patch((n) => ({ font: { ...n.font!, ...change } }));
  return (
    <Section title={t("design.typography")}>
      <StyleBar kind="text" nodes={nodes} patch={patch} />
      <div className="design-row">
        <select aria-label={t("design.font")} className="design-grow" value={family === MIXED ? "" : family} onChange={(e) => font({ family: e.target.value })}>
          {family === MIXED && <option value="">{t("design.mixed")}</option>}
          {[...new Set([...(family !== MIXED ? [family] : []), ...FONTS])].map((f) => (
            <option key={f} value={f}>
              {f}
            </option>
          ))}
        </select>
      </div>
      <div className="design-row">
        <select aria-label={t("design.weight")} value={weight === MIXED ? "" : String(weight)} onChange={(e) => font({ weight: Number(e.target.value) })}>
          {weight === MIXED && <option value="">{t("design.mixed")}</option>}
          {WEIGHTS.map((w) => (
            <option key={w} value={w}>
              {w}
            </option>
          ))}
        </select>
        <NumberField label="Aa" title={t("design.fontSize")} value={size} min={1} max={2000} onChange={(v) => font({ size: v })} />
      </div>
      <div className="design-row">
        <NumberField label="↕" title={t("design.lineHeight")} value={lh} min={0} onChange={(v) => patch(() => ({ line_height: v }))} />
        <NumberField label="↔" title={t("design.letterSpacing")} value={ls} step={0.1} onChange={(v) => patch(() => ({ letter_spacing: v }))} />
      </div>
      <div className="design-row">
        <div className="design-seg" role="group" aria-label={t("design.textAlign")}>
          {(["left", "center", "right"] as const).map((a) => (
            <button key={a} type="button" aria-pressed={align === a} data-on={align === a || undefined} onClick={() => patch(() => ({ align: a }))} aria-label={t(`design.align.${a}` as never)}>
              {a === "left" ? "⇤" : a === "center" ? "↔" : "⇥"}
            </button>
          ))}
        </div>
        <select aria-label={t("design.autoResize")} value={auto === MIXED ? "" : auto} onChange={(e) => patch((n) => ({ auto_resize: e.target.value as DesignNode["auto_resize"], ...(e.target.value === "none" ? { width: n.width, height: n.height } : {}) }))}>
          {auto === MIXED && <option value="">{t("design.mixed")}</option>}
          <option value="width">{t("design.autoWidth")}</option>
          <option value="height">{t("design.autoHeight")}</option>
          <option value="none">{t("design.fixedSize")}</option>
        </select>
      </div>
    </Section>
  );
}

type Patcher = (fn: (n: DesignNode) => Partial<DesignNode>) => void;

const NEW_LAYOUT = { dir: "column", gap: 8, pad: [0, 0, 0, 0], main: "start", cross: "start" } as const;

function Select<T extends string>({ label, value, options, onChange }: { label: string; value: Value<T>; options: [T, string][]; onChange: (v: T) => void }) {
  return (
    <select aria-label={label} title={label} value={value === MIXED ? "" : value} onChange={(e) => onChange(e.target.value as T)}>
      {value === MIXED && <option value="">{t("design.mixed")}</option>}
      {options.map(([v, text]) => (
        <option key={v} value={v}>
          {text}
        </option>
      ))}
    </select>
  );
}

const sizing = (label: string, v: string) => label + " · " + t(("design." + (v === "fill" ? "sizeFill" : v)) as never);

/** Auto layout of a frame: direction, spacing, padding, alignment and how it sizes itself. */
function LayoutProps({ nodes, patch }: { nodes: DesignNode[]; patch: Patcher }) {
  const withLayout = nodes.every((n) => n.layout);
  const edit = (change: Partial<NonNullable<DesignNode["layout"]>>) => patch((n) => ({ layout: { ...(n.layout ?? NEW_LAYOUT), ...change, pad: change.pad ?? n.layout?.pad ?? [0, 0, 0, 0] } as NonNullable<DesignNode["layout"]> }));
  const pad = (i: number) => (v: number) => patch((n) => ({ layout: { ...(n.layout ?? NEW_LAYOUT), pad: (n.layout?.pad ?? [0, 0, 0, 0]).map((p, j) => (j === i ? v : p)) as [number, number, number, number] } as NonNullable<DesignNode["layout"]> }));
  const action = withLayout ? (
    <button type="button" className="icon-button design-mini" aria-label={t("design.removeLayout")} title={t("design.removeLayout")} onClick={() => patch(() => ({ layout: undefined, sizing_x: undefined, sizing_y: undefined }))}>
      −
    </button>
  ) : (
    <AddButton label={t("design.addLayout")} onClick={() => patch((n) => ({ layout: n.layout ?? ({ ...NEW_LAYOUT, pad: [0, 0, 0, 0] } as NonNullable<DesignNode["layout"]>) }))} />
  );
  return (
    <Section title={t("design.autoLayout")} action={action}>
      {withLayout && (
        <>
          <div className="design-row">
            <div className="design-seg" role="group" aria-label={t("design.direction")}>
              {(["column", "row"] as const).map((d) => {
                const on = shared(nodes, (n) => n.layout?.dir) === d;
                return (
                  <button key={d} type="button" aria-pressed={on} data-on={on || undefined} aria-label={t(`design.dir.${d}` as never)} title={t(`design.dir.${d}` as never)} onClick={() => edit({ dir: d })}>
                    {d === "column" ? "↓" : "→"}
                  </button>
                );
              })}
            </div>
            <NumberField label="⇔" title={t("design.gap")} value={shared(nodes, (n) => n.layout?.gap ?? 0)} min={0} onChange={(v) => edit({ gap: v })} />
          </div>
          <div className="design-grid">
            <NumberField label="T" title={t("design.padTop")} value={shared(nodes, (n) => n.layout?.pad[0] ?? 0)} min={0} onChange={pad(0)} />
            <NumberField label="R" title={t("design.padRight")} value={shared(nodes, (n) => n.layout?.pad[1] ?? 0)} min={0} onChange={pad(1)} />
            <NumberField label="B" title={t("design.padBottom")} value={shared(nodes, (n) => n.layout?.pad[2] ?? 0)} min={0} onChange={pad(2)} />
            <NumberField label="L" title={t("design.padLeft")} value={shared(nodes, (n) => n.layout?.pad[3] ?? 0)} min={0} onChange={pad(3)} />
          </div>
          <div className="design-row">
            <Select label={t("design.mainAlign")} value={shared(nodes, (n) => n.layout!.main)} onChange={(v) => edit({ main: v })} options={[["start", t("design.start")], ["center", t("design.center")], ["end", t("design.end")], ["between", t("design.between")]]} />
            <Select label={t("design.crossAlign")} value={shared(nodes, (n) => n.layout!.cross)} onChange={(v) => edit({ cross: v })} options={[["start", t("design.start")], ["center", t("design.center")], ["end", t("design.end")], ["stretch", t("design.stretch")]]} />
          </div>
        </>
      )}
      <div className="design-row">
        <Select label={t("design.widthMode")} value={shared(nodes, (n) => n.sizing_x ?? "fixed")} onChange={(v) => patch(() => ({ sizing_x: v === "fixed" ? undefined : v }))} options={[["fixed", sizing("W", "fixed")], ["hug", sizing("W", "hug")], ["fill", sizing("W", "fill")]]} />
        <Select label={t("design.heightMode")} value={shared(nodes, (n) => n.sizing_y ?? "fixed")} onChange={(v) => patch(() => ({ sizing_y: v === "fixed" ? undefined : v }))} options={[["fixed", sizing("H", "fixed")], ["hug", sizing("H", "hug")], ["fill", sizing("H", "fill")]]} />
      </div>
    </Section>
  );
}

/** How a child sits in its parent: in the layout's flow, or pinned by constraints. */
function ChildProps({ nodes, parents, patch }: { nodes: DesignNode[]; parents: (DesignNode | null)[]; patch: Patcher }) {
  const inLayout = parents.every((p) => p?.layout);
  const inFrame = parents.every((p) => p && !p.layout && p.type !== "group");
  if (!inLayout && !inFrame) return null;
  if (inLayout) {
    return (
      <Section title={t("design.inLayout")}>
        <div className="design-row">
          <Select label={t("design.widthMode")} value={shared(nodes, (n) => n.sizing_x ?? "fixed")} onChange={(v) => patch(() => ({ sizing_x: v === "fixed" ? undefined : v }))} options={[["fixed", sizing("W", "fixed")], ["fill", sizing("W", "fill")]]} />
          <Select label={t("design.heightMode")} value={shared(nodes, (n) => n.sizing_y ?? "fixed")} onChange={(v) => patch(() => ({ sizing_y: v === "fixed" ? undefined : v }))} options={[["fixed", sizing("H", "fixed")], ["fill", sizing("H", "fill")]]} />
        </div>
        <label className="design-check">
          <input type="checkbox" checked={shared(nodes, (n) => n.absolute === true) === true} onChange={(e) => patch(() => ({ absolute: e.target.checked ? true : undefined }))} />
          {t("design.absolute")}
        </label>
      </Section>
    );
  }
  return (
    <Section title={t("design.constraints")}>
      <div className="design-row">
        <Select label={t("design.constraintX")} value={shared(nodes, (n) => n.constraints?.x ?? "left")} onChange={(v) => patch((n) => ({ constraints: { x: v, y: n.constraints?.y ?? "top" } }))} options={[["left", t("design.left")], ["right", t("design.right")], ["center", t("design.center")], ["stretch", t("design.stretch")], ["scale", t("design.scale")]]} />
        <Select label={t("design.constraintY")} value={shared(nodes, (n) => n.constraints?.y ?? "top")} onChange={(v) => patch((n) => ({ constraints: { x: n.constraints?.x ?? "left", y: v } }))} options={[["top", t("design.top")], ["bottom", t("design.bottom")], ["center", t("design.center")], ["stretch", t("design.stretch")], ["scale", t("design.scale")]]} />
      </div>
    </Section>
  );
}

/** Colour variables: modes, and a colour per mode. */
function Variables({ doc, commit }: { doc: DesignDoc; commit: (d: DesignDoc) => void }) {
  const v = doc.variables;
  const [mode, setMode] = useState("");
  const items = Object.entries(v?.items ?? {});
  const set = (next: NonNullable<DesignDoc["variables"]>) => commit({ ...doc, variables: next });
  const base = v ?? { modes: ["Light"], mode: "Light", items: {} };
  const addMode = () => {
    const name = mode.trim().slice(0, 40);
    if (!name || base.modes.includes(name)) return;
    setMode("");
    set({ ...base, modes: [...base.modes, name], items: Object.fromEntries(Object.entries(base.items).map(([id, it]) => [id, { ...it, values: { ...it.values, [name]: it.values[base.mode] ?? "#000000" } }])) });
  };
  return (
    <Section title={t("design.variables")} action={<AddButton label={t("design.addVariable")} onClick={() => set({ ...base, items: { ...base.items, [newId()]: { name: `${t("design.color")} ${items.length + 1}`, values: Object.fromEntries(base.modes.map((m) => [m, "#808080"])) } } })} />}>
      <div className="design-row">
        <select aria-label={t("design.mode")} className="design-grow" value={base.mode} onChange={(e) => set({ ...base, mode: e.target.value })}>
          {base.modes.map((m) => (
            <option key={m} value={m}>
              {m}
            </option>
          ))}
        </select>
        <input className="design-name" aria-label={t("design.addMode")} placeholder={t("design.addMode")} value={mode} maxLength={40} onChange={(e) => setMode(e.target.value)} onKeyDown={(e) => e.key === "Enter" && addMode()} />
      </div>
      {items.map(([id, it]) => (
        <div className="design-row" key={id}>
          <input className="design-name" aria-label={t("design.variableName")} value={it.name} maxLength={80} onChange={(e) => set({ ...base, items: { ...base.items, [id]: { ...it, name: e.target.value } } })} />
          <ColorField label={it.name} value={it.values[base.mode] ?? "#000000"} onChange={(c) => set({ ...base, items: { ...base.items, [id]: { ...it, values: { ...it.values, [base.mode]: c.slice(0, 7) } } } })} />
          <button type="button" className="design-row-tool" aria-label={`${t("design.remove")} ${it.name}` } onClick={() => set({ ...base, items: Object.fromEntries(Object.entries(base.items).filter(([k]) => k !== id)) })}>
            −
          </button>
        </div>
      ))}
    </Section>
  );
}

/** Shared styles of the document, with a way to delete one. */
function StyleList({ doc, commit }: { doc: DesignDoc; commit: (d: DesignDoc) => void }) {
  const rows: { kind: "fills" | "text" | "effects"; id: string; name: string }[] = [];
  for (const kind of ["fills", "text", "effects"] as const) for (const [id, s] of Object.entries(doc.styles?.[kind] ?? {})) rows.push({ kind, id, name: (s as { name: string }).name });
  if (rows.length === 0) return null;
  const drop = (kind: "fills" | "text" | "effects", id: string) => {
    const group = Object.fromEntries(Object.entries(doc.styles?.[kind] ?? {}).filter(([k]) => k !== id));
    commit({ ...doc, styles: { ...doc.styles, [kind]: Object.keys(group).length ? group : undefined } } as DesignDoc);
  };
  return (
    <Section title={t("design.styles")}>
      {rows.map((r) => (
        <div className="design-row" key={r.kind + r.id}>
          <span className="design-row-name">{r.name}</span>
          <span className="design-muted">{t(`design.style.${r.kind === "fills" ? "fill" : r.kind === "text" ? "text" : "effect"}` as never)}</span>
          <button type="button" className="design-row-tool" aria-label={`${t("design.remove")} ${r.name}`} onClick={() => drop(r.kind, r.id)}>
            −
          </button>
        </div>
      ))}
    </Section>
  );
}

/** Boolean operations, flatten, mask and point editing for the selected shapes. */
function ShapeOps({ nodes }: { nodes: DesignNode[] }) {
  const store = useDesign.getState;
  const shapes = nodes.filter((n) => n.type !== "text" && n.type !== "instance");
  const bool = nodes.length === 1 && nodes[0]!.type === "group" && nodes[0]!.bool_op ? nodes[0]!.bool_op : null;
  const canBool = nodes.length >= 2 ? shapes.length === nodes.length : !!bool;
  const flattenable = shapes.length === nodes.length && nodes.every((n) => n.type !== "path" && n.type !== "group" && !n.children?.length);
  const path = nodes.length === 1 && nodes[0]!.type === "path" ? nodes[0]! : null;
  if (!canBool && !flattenable && !path && shapes.length === 0) return null;
  return (
    <Section title={t("design.boolean")}>
      {canBool && (
        <div className="design-row design-wrap">
          {(["union", "subtract", "intersect", "exclude"] as const).map((op) => (
            <button key={op} type="button" className="design-chip" aria-pressed={bool === op} data-on={bool === op || undefined} onClick={() => store().boolean(op)}>
              {t((`design.${op}`) as never)}
            </button>
          ))}
        </div>
      )}
      <div className="design-row design-wrap">
        {flattenable && (
          <button type="button" className="design-chip" onClick={() => store().flatten()}>
            {t("design.flatten")}
          </button>
        )}
        {path && (
          <button type="button" className="design-chip" onClick={() => store().setEditPath(path.id)}>
            {t("design.editPoints")}
          </button>
        )}
      </div>
      {shapes.length > 0 && (
        <label className="design-check">
          <input type="checkbox" checked={shared(shapes, (n) => n.mask === true) === true} onChange={(e) => store().patch(shapes.map((n) => n.id), { mask: e.target.checked ? true : undefined })} />
          {t("design.mask")}
        </label>
      )}
    </Section>
  );
}

const TRIGGER_LIST = ["click", "hover", "press", "drag", "after", "key"] as const;
const ACTION_LIST = ["navigate", "overlay", "back", "close", "url", "scroll_to"] as const;
const TRANSITION_LIST = ["dissolve", "instant", "smart", "slide", "push", "move"] as const;

/** Prototype links of one layer: what a click, a hover or a key does in the presentation. */
function PrototypeProps({ node, doc, patch }: { node: DesignNode; doc: DesignDoc; patch: Patcher }) {
  const frames = pageOf(doc, useDesign.getState().pageId).children.filter((c) => c.type === "frame");
  const list = node.interactions ?? [];
  const edit = (i: number, change: Partial<Interaction>) => patch((n) => ({ interactions: (n.interactions ?? []).map((x, k) => (k === i ? ({ ...x, ...change } as Interaction) : x)) }));
  const add = () => patch((n) => ({ interactions: [...(n.interactions ?? []), { trigger: "click", action: "navigate", to: frames.find((f) => f.id !== n.id)?.id ?? "", transition: "dissolve", duration: 300 } as Interaction] }));
  const opt = (kind: string, v: string) => [v, t((`design.proto.${kind}.${v}`) as never)] as [string, string];
  return (
    <Section title={t("design.prototype")} action={<AddButton label={t("design.addInteraction")} onClick={add} />}>
      {node.type === "frame" && (
        <label className="design-check">
          <input type="checkbox" checked={node.flow_start === true} onChange={(e) => patch(() => ({ flow_start: e.target.checked ? true : undefined }))} />
          {t("design.flowStart")}
        </label>
      )}
      {(node.type === "frame" || node.type === "component") && (
        <div className="design-row">
          <Select label={t("design.scrolling")} value={node.scroll ?? "none"} onChange={(v) => patch(() => ({ scroll: v === "none" ? undefined : (v as "vertical" | "horizontal" | "both") }))} options={[["none", t("design.scroll.none")], ["vertical", t("design.scroll.vertical")], ["horizontal", t("design.scroll.horizontal")], ["both", t("design.scroll.both")]]} />
        </div>
      )}
      {list.map((x, i) => (
        <div className="design-interaction" key={i}>
          <div className="design-row">
            <Select label={t("design.proto.trigger")} value={x.trigger} onChange={(v) => edit(i, { trigger: v })} options={TRIGGER_LIST.map((v) => opt("trigger", v)) as [Interaction["trigger"], string][]} />
            <Select label={t("design.proto.action")} value={x.action} onChange={(v) => edit(i, { action: v })} options={ACTION_LIST.map((v) => opt("action", v)) as [Interaction["action"], string][]} />
            <button type="button" className="design-row-tool" aria-label={t("design.removeInteraction")} onClick={() => patch((n) => ({ interactions: (n.interactions ?? []).filter((_, k) => k !== i) }))}>
              −
            </button>
          </div>
          {(x.action === "navigate" || x.action === "overlay") && (
            <div className="design-row">
              <select aria-label={t("design.proto.to")} className="design-grow" value={x.to ?? ""} onChange={(e) => edit(i, { to: e.target.value })}>
                {!frames.some((f) => f.id === x.to) && <option value="">{t("design.proto.pick")}</option>}
                {frames.map((f) => (
                  <option key={f.id} value={f.id}>
                    {f.name}
                  </option>
                ))}
              </select>
            </div>
          )}
          {x.action === "url" && <input className="design-name" aria-label={t("design.proto.url")} placeholder="https://" value={x.url ?? ""} onChange={(e) => edit(i, { url: e.target.value })} />}
          {x.trigger === "key" && <input className="design-name" aria-label={t("design.proto.key")} placeholder="Enter, ArrowRight…" value={x.key ?? ""} maxLength={20} onChange={(e) => edit(i, { key: e.target.value })} />}
          {x.trigger === "after" && <NumberField label="⏱" title={t("design.proto.delay")} value={x.delay ?? 0} min={0} suffix="ms" onChange={(v) => edit(i, { delay: v })} />}
          {(x.action === "navigate" || x.action === "overlay" || x.action === "back") && (
            <div className="design-row">
              <Select label={t("design.proto.transition")} value={x.transition ?? "dissolve"} onChange={(v) => edit(i, { transition: v })} options={TRANSITION_LIST.map((v) => opt("transition", v)) as [NonNullable<Interaction["transition"]>, string][]} />
              <NumberField label="ms" title={t("design.proto.duration")} value={x.duration ?? 300} min={0} max={10000} onChange={(v) => edit(i, { duration: v })} />
            </div>
          )}
        </div>
      ))}
    </Section>
  );
}

/** Handoff: the code that describes a layer. */
function CodeProps({ node, parent }: { node: DesignNode; parent: DesignNode | null }) {
  const [kind, setKind] = useState<CodeKind>("css");
  const [copied, setCopied] = useState(false);
  const code = codeFor(node, kind, parent);
  return (
    <Section
      title={t("design.code")}
      action={
        <button
          type="button"
          className="design-chip"
          onClick={() => {
            void navigator.clipboard?.writeText(code).then(() => {
              setCopied(true);
              setTimeout(() => setCopied(false), 1200);
            });
          }}
        >
          {copied ? t("design.copied") : t("design.copy")}
        </button>
      }
    >
      <div className="design-row">
        <Select label={t("design.code")} value={kind} onChange={setKind} options={[["css", "CSS"], ["tailwind", "Tailwind"], ["swiftui", "SwiftUI"], ["compose", "Compose"]]} />
      </div>
      <pre className="design-code" tabIndex={0} aria-label={t("design.code")}>
        {code}
      </pre>
    </Section>
  );
}

/** The variant an instance shows: one choice per property of its component set. */
function VariantProps({ instance, doc, patch }: { instance: DesignNode; doc: DesignDoc; patch: Patcher }) {
  const masters = components(doc);
  const own = masters.find((m) => m.id === instance.component_id);
  if (!own?.set) return null;
  const set = masters.filter((m) => m.set === own.set);
  const props = [...new Set(set.flatMap((m) => Object.keys(m.variant ?? {})))];
  if (props.length === 0) return null;
  const shown = masterFor(instance, masters);
  return (
    <Section title={own.set}>
      {props.map((p) => (
        <div className="design-row" key={p}>
          <span className="design-muted">{p}</span>
          <select aria-label={p} className="design-grow" value={instance.variant?.[p] ?? shown?.variant?.[p] ?? ""} onChange={(e) => patch((n) => ({ variant: { ...(n.variant ?? shown?.variant ?? {}), [p]: e.target.value } }))}>
            {[...new Set(set.map((m) => m.variant?.[p]).filter((v): v is string => !!v))].map((v) => (
              <option key={v} value={v}>
                {v}
              </option>
            ))}
          </select>
        </div>
      ))}
    </Section>
  );
}

const ALIGNS: AlignKind[] = ["left", "hcenter", "right", "top", "vcenter", "bottom"];

function Export() {
  const [format, setFormat] = useState<ExportFormat>("png");
  const [scale, setScale] = useState(2);
  const [busy, setBusy] = useState(false);
  const selection = useDesign((s) => s.selection);
  return (
    <Section title={t("design.export")}>
      <div className="design-row">
        <select aria-label={t("design.exportScale")} value={scale} disabled={format !== "png"} onChange={(e) => setScale(Number(e.target.value))}>
          {[0.5, 1, 2, 3, 4].map((v) => (
            <option key={v} value={v}>
              {v}x
            </option>
          ))}
        </select>
        <select aria-label={t("design.exportFormat")} value={format} onChange={(e) => setFormat(e.target.value as ExportFormat)}>
          <option value="png">PNG</option>
          <option value="svg">SVG</option>
          <option value="pdf">PDF</option>
        </select>
      </div>
      <button
        type="button"
        className="design-button"
        disabled={busy}
        onClick={async () => {
          setBusy(true);
          try {
            await runExport(format, scale);
          } finally {
            setBusy(false);
          }
        }}
      >
        {selection.length ? t("design.exportSelection") : t("design.exportPage")}
      </button>
    </Section>
  );
}

export function Inspector() {
  const doc = useDesign((s) => s.doc);
  const pageId = useDesign((s) => s.pageId);
  const selection = useDesign((s) => s.selection);
  const patchStore = useDesign((s) => s.patch);
  const alignStore = useDesign((s) => s.align);
  const distribute = useDesign((s) => s.distribute);
  const commit = useDesign((s) => s.commit);
  if (!doc) return <aside className="design-right" />;
  const page = pageOf(doc, pageId);
  const nodes = selection.map((id) => locate(page, id)?.node).filter((n): n is DesignNode => !!n);
  const ids = nodes.map((n) => n.id);
  const patch = (fn: (n: DesignNode) => Partial<DesignNode>) => patchStore(ids, fn);

  if (nodes.length === 0) {
    return (
      <aside className="design-right" aria-label={t("design.properties")}>
        <Section title={t("design.page")}>
          <div className="design-row">
            <ColorField label={t("design.background")} value={page.background} onChange={(c) => commit({ ...doc, pages: doc.pages.map((p) => (p.id === page.id ? { ...p, background: c.slice(0, 7) } : p)) })} />
          </div>
        </Section>
        <Variables doc={doc} commit={commit} />
        <StyleList doc={doc} commit={commit} />
        <Export />
      </aside>
    );
  }

  const one = nodes.length === 1 ? nodes[0]! : null;
  const types = new Set(nodes.map((n) => n.type));
  const has = (...ts: DesignNode["type"][]) => nodes.every((n) => ts.includes(n.type));
  const rounded = has("rect", "frame", "component", "instance", "image");
  const textOnly = has("text");
  // Auto-sized text shows the size it is drawn at.
  const num = (key: "x" | "y" | "width" | "height" | "rotation" | "opacity") => shared(nodes, (n) => (key === "rotation" ? (n.rotation ?? 0) : key === "opacity" ? Math.round((n.opacity ?? 1) * 100) : key === "width" || key === "height" ? sizeOf(n)[key] : n[key]));
  const setNum = (key: "x" | "y" | "width" | "height") => (v: number) => patch((n) => ({ [key]: v, ...(n.type === "text" && (key === "width" || key === "height") ? { auto_resize: key === "width" ? "height" : "none" } : {}) }) as Partial<DesignNode>);

  return (
    <aside className="design-right" aria-label={t("design.properties")}>
      <div className="design-alignbar" role="toolbar" aria-label={t("design.alignment")}>
        {ALIGNS.map((a) => {
          const I = IconAlign[a];
          return (
            <button key={a} type="button" className="icon-button" aria-label={t(`design.align.${a}` as never)} title={t(`design.align.${a}` as never)} onClick={() => alignStore(a)}>
              <I />
            </button>
          );
        })}
        <button type="button" className="icon-button" aria-label={t("design.distributeH")} title={t("design.distributeH")} disabled={nodes.length < 3} onClick={() => distribute("x")}>
          <IconDistH />
        </button>
        <button type="button" className="icon-button" aria-label={t("design.distributeV")} title={t("design.distributeV")} disabled={nodes.length < 3} onClick={() => distribute("y")}>
          <IconDistV />
        </button>
      </div>
      <Section title={one ? t(`design.type.${one.type}` as never) : `${nodes.length} ${t("design.selected")}`}>
        <div className="design-grid">
          <NumberField label="X" value={num("x")} onChange={setNum("x")} />
          <NumberField label="Y" value={num("y")} onChange={setNum("y")} />
          <NumberField label="W" title={t("design.width")} value={num("width")} min={0} onChange={setNum("width")} />
          <NumberField label="H" title={t("design.height")} value={num("height")} min={0} onChange={setNum("height")} />
          <NumberField label="↻" title={t("design.rotation")} value={num("rotation")} suffix="°" onChange={(v) => patch(() => ({ rotation: ((v % 360) + 360) % 360 }))} />
          {rounded && <NumberField label="◜" title={t("design.radius")} value={shared(nodes, (n) => (typeof n.radius === "number" ? n.radius : (n.radius?.[0] ?? 0)))} min={0} onChange={(v) => patch(() => ({ radius: v }))} />}
        </div>
        <div className="design-row">
          <NumberField label="◐" title={t("design.opacity")} value={num("opacity")} min={0} max={100} suffix="%" onChange={(v) => patch(() => ({ opacity: v / 100 }))} />
          {has("frame", "component") && (
            <label className="design-check">
              <input type="checkbox" checked={shared(nodes, (n) => n.clip !== false) === true} onChange={(e) => patch(() => ({ clip: e.target.checked }))} />
              {t("design.clip")}
            </label>
          )}
        </div>
        {has("polygon", "star") && (
          <div className="design-row">
            <NumberField label="#" title={t("design.sides")} value={shared(nodes, (n) => n.sides ?? 3)} min={3} max={100} onChange={(v) => patch(() => ({ sides: Math.round(v) }))} />
            {has("star") && <NumberField label="◌" title={t("design.innerRadius")} value={shared(nodes, (n) => Math.round((n.inner ?? 0.38) * 100))} min={1} max={100} suffix="%" onChange={(v) => patch(() => ({ inner: v / 100 }))} />}
          </div>
        )}
        {has("image") && (
          <div className="design-row">
            <select aria-label={t("design.imageFit")} value={shared(nodes, (n) => n.fit ?? "cover") === MIXED ? "" : (nodes[0]!.fit ?? "cover")} onChange={(e) => patch(() => ({ fit: e.target.value as DesignNode["fit"] }))}>
              <option value="cover">{t("design.fitCover")}</option>
              <option value="contain">{t("design.fitContain")}</option>
              <option value="stretch">{t("design.fitStretch")}</option>
            </select>
          </div>
        )}
        {one?.type === "instance" && (
          <div className="design-row">
            <button type="button" className="design-chip" onClick={() => useDesign.getState().detach()}>
              {t("design.detach")}
            </button>
            <button
              type="button"
              className="design-chip"
              onClick={() => {
                const master = doc.pages.map((p) => ({ p, at: locate(p, one.component_id ?? "") })).find((x) => x.at);
                if (master) {
                  useDesign.getState().setPage(master.p.id);
                  useDesign.getState().select([master.at!.node.id]);
                }
              }}
            >
              {t("design.goToComponent")}
            </button>
          </div>
        )}
      </Section>
      {has("frame", "component") && <LayoutProps nodes={nodes} patch={patch} />}
      <ShapeOps nodes={nodes} />
      {one && one.type !== "group" && <PrototypeProps node={one} doc={doc} patch={patch} />}
      <ChildProps nodes={nodes} parents={nodes.map((n) => locate(page, n.id)?.parent ?? null)} patch={patch} />
      {one?.type === "instance" && <VariantProps instance={one} doc={doc} patch={patch} />}
      {textOnly && <TextProps nodes={nodes} patch={(fn) => patch((n) => ({ text_style: undefined, ...fn(n) }))} />}
      {(!types.has("group") || (one?.type === "group" && one.bool_op)) && <Fills nodes={nodes} patch={patch} />}
      {!types.has("group") && !textOnly && <Strokes nodes={nodes} patch={patch} />}
      <Effects nodes={nodes} patch={(fn) => patch((n) => ({ effect_style: undefined, ...fn(n) }))} />
      {one && <CodeProps node={one} parent={locate(page, one.id)?.parent ?? null} />}
      <Export />
    </aside>
  );
}
