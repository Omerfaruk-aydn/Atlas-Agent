// Developer handoff: the CSS, Tailwind, SwiftUI and Jetpack Compose that describe a layer.

import type { DesignNode, Paint } from "./model";
import { splitColor } from "./model";
import { sizeOf } from "./text";

export type CodeKind = "css" | "tailwind" | "swiftui" | "compose";

const r = (v: number) => Math.round(v * 100) / 100;
const px = (v: number) => `${r(v)}px`;

function rgba(hex: string): string {
  const { rgb, alpha } = splitColor(hex);
  if (alpha >= 1) return rgb;
  const n = parseInt(rgb.slice(1), 16);
  return `rgba(${(n >> 16) & 255}, ${(n >> 8) & 255}, ${n & 255}, ${r(alpha)})`;
}

function cssPaint(p: Paint): string {
  if (p.type === "solid") return rgba(p.color);
  if (p.type === "image") return `url(${p.src.startsWith("data:") ? "…" : p.src}) center / ${p.fit === "contain" ? "contain" : p.fit === "stretch" ? "100% 100%" : "cover"} no-repeat`;
  const stops = p.stops.map((s) => `${rgba(s.color)} ${r(s.offset * 100)}%`).join(", ");
  return p.type === "radial" ? `radial-gradient(circle, ${stops})` : `linear-gradient(${r((p.angle ?? 90) + 90)}deg, ${stops})`;
}

const radiusOf = (n: DesignNode): number[] | null => {
  const v = n.radius;
  if (v === undefined || (typeof v === "number" && v === 0)) return null;
  return typeof v === "number" ? [v] : v;
};

/** CSS declarations for a layer, as the parent's layout (if any) places it. */
export function cssDeclarations(n: DesignNode, parent?: DesignNode | null): [string, string][] {
  const d: [string, string][] = [];
  const size = sizeOf(n);
  const inFlow = !!parent?.layout && !n.absolute;
  if (!inFlow) {
    if (parent) d.push(["position", "absolute"]);
    if (parent) {
      d.push(["left", px(n.x)]);
      d.push(["top", px(n.y)]);
    }
  }
  const fillX = inFlow && n.sizing_x === "fill";
  const fillY = inFlow && n.sizing_y === "fill";
  if (parent?.layout?.dir === "row" ? fillX : fillY) d.push(["flex", "1 1 0"]);
  d.push(["width", n.sizing_x === "hug" ? "fit-content" : fillX && parent?.layout?.dir !== "row" ? "100%" : px(size.width)]);
  if (n.type !== "text" || n.auto_resize === "none") d.push(["height", n.sizing_y === "hug" ? "fit-content" : fillY && parent?.layout?.dir === "row" ? "100%" : px(size.height)]);
  if (n.rotation) d.push(["transform", `rotate(${r(n.rotation)}deg)`]);
  if (n.opacity !== undefined && n.opacity < 1) d.push(["opacity", String(r(n.opacity))]);
  const rad = radiusOf(n);
  if (rad) d.push(["border-radius", rad.map(px).join(" ")]);
  const fills = (n.fills ?? []).filter((f) => f.visible !== false);
  if (n.type === "text") {
    if (fills[0]) d.push(["color", cssPaint(fills[0])]);
    const f = n.font;
    if (f) {
      d.push(["font-family", `'${f.family}', sans-serif`]);
      d.push(["font-size", px(f.size)]);
      d.push(["font-weight", String(f.weight)]);
      if (f.style === "italic") d.push(["font-style", "italic"]);
    }
    if (n.line_height) d.push(["line-height", px(n.line_height)]);
    if (n.letter_spacing) d.push(["letter-spacing", px(n.letter_spacing)]);
    if (n.align && n.align !== "left") d.push(["text-align", n.align]);
    if (n.decoration === "underline") d.push(["text-decoration", "underline"]);
    if (n.decoration === "strike") d.push(["text-decoration", "line-through"]);
    if (n.transform === "upper") d.push(["text-transform", "uppercase"]);
    if (n.transform === "lower") d.push(["text-transform", "lowercase"]);
  } else if (fills.length) d.push(["background", fills.map(cssPaint).reverse().join(", ")]);
  const stroke = (n.strokes ?? []).find((s) => s.visible !== false && s.width > 0);
  if (stroke && n.type !== "text") {
    d.push(["border", `${px(stroke.width)} ${stroke.dash?.length ? "dashed" : "solid"} ${rgba(stroke.color)}`]);
    d.push(["box-sizing", "border-box"]);
  }
  const shadows = (n.effects ?? []).filter((e) => e.visible !== false && e.type !== "blur");
  if (shadows.length) d.push(["box-shadow", shadows.map((e) => `${e.type === "inner-shadow" ? "inset " : ""}${px(e.x ?? 0)} ${px(e.y ?? 0)} ${px(e.blur)} ${px(e.spread ?? 0)} ${rgba(e.color ?? "#00000040")}`).join(", ")]);
  const blur = (n.effects ?? []).find((e) => e.visible !== false && e.type === "blur");
  if (blur) d.push(["filter", `blur(${px(blur.blur / 2)})`]);
  if (n.clip && n.type !== "text" && n.children?.length) d.push(["overflow", "hidden"]);
  const l = n.layout;
  if (l) {
    d.push(["display", "flex"]);
    d.push(["flex-direction", l.dir]);
    if (l.gap) d.push(["gap", px(l.gap)]);
    if (l.pad.some((v) => v)) d.push(["padding", l.pad.map(px).join(" ")]);
    const main = { start: "flex-start", center: "center", end: "flex-end", between: "space-between" }[l.main];
    const cross = { start: "flex-start", center: "center", end: "flex-end", stretch: "stretch" }[l.cross];
    d.push(["justify-content", main]);
    d.push(["align-items", cross]);
  } else if (n.children?.length && n.type !== "group") d.push(["position", "relative"]);
  return d;
}

const css = (n: DesignNode, parent?: DesignNode | null) => cssDeclarations(n, parent).map(([k, v]) => `${k}: ${v};`).join("\n");

function tailwind(n: DesignNode, parent?: DesignNode | null): string {
  const out: string[] = [];
  for (const [k, v] of cssDeclarations(n, parent)) {
    const val = v.replace(/\s+/g, "_");
    switch (k) {
      case "position":
        out.push(v);
        break;
      case "left":
      case "top":
        out.push(`${k}-[${val}]`);
        break;
      case "width":
        out.push(v === "100%" ? "w-full" : v === "fit-content" ? "w-fit" : `w-[${val}]`);
        break;
      case "height":
        out.push(v === "100%" ? "h-full" : v === "fit-content" ? "h-fit" : `h-[${val}]`);
        break;
      case "flex":
        out.push("flex-1");
        break;
      case "display":
        out.push("flex");
        break;
      case "flex-direction":
        out.push(v === "row" ? "flex-row" : "flex-col");
        break;
      case "gap":
        out.push(`gap-[${val}]`);
        break;
      case "padding":
        out.push(`p-[${val}]`);
        break;
      case "justify-content":
        out.push(`justify-${v.replace("flex-", "").replace("space-", "")}`);
        break;
      case "align-items":
        out.push(`items-${v.replace("flex-", "")}`);
        break;
      case "background":
        out.push(`bg-[${val}]`);
        break;
      case "color":
        out.push(`text-[${val}]`);
        break;
      case "border-radius":
        out.push(`rounded-[${val}]`);
        break;
      case "border":
        out.push(`border-[${val}]`);
        break;
      case "opacity":
        out.push(`opacity-[${v}]`);
        break;
      case "box-shadow":
        out.push(`shadow-[${val}]`);
        break;
      case "font-size":
        out.push(`text-[${val}]`);
        break;
      case "font-weight":
        out.push(`font-[${v}]`);
        break;
      case "line-height":
        out.push(`leading-[${val}]`);
        break;
      case "letter-spacing":
        out.push(`tracking-[${val}]`);
        break;
      case "overflow":
        out.push("overflow-hidden");
        break;
      case "text-align":
        out.push(`text-${v}`);
        break;
      case "font-family":
        out.push(`font-['${n.font?.family.replace(/\s+/g, "_")}']`);
        break;
      case "transform":
        out.push(`rotate-[${n.rotation}deg]`);
        break;
    }
  }
  return out.join(" ");
}

const hex8 = (c: string) => {
  const { rgb, alpha } = splitColor(c);
  return { rgb: rgb.slice(1), a: Math.round(alpha * 255).toString(16).padStart(2, "0").toUpperCase() };
};

function swiftui(n: DesignNode, depth = 0, parent?: DesignNode | null): string {
  const pad = "    ".repeat(depth);
  const size = sizeOf(n);
  const fill = (n.fills ?? []).find((f) => f.visible !== false);
  const color = fill?.type === "solid" ? `Color(hex: "#${hex8(fill.color).rgb}")${hex8(fill.color).a !== "FF" ? `.opacity(${r(splitColor(fill.color).alpha)})` : ""}` : fill ? "LinearGradient(colors: [/* stops */], startPoint: .top, endPoint: .bottom)" : "Color.clear";
  const rad = radiusOf(n);
  const frame = `.frame(width: ${r(size.width)}, height: ${r(size.height)})`;
  const stroke = (n.strokes ?? []).find((s) => s.visible !== false && s.width > 0);
  const strokeMod = stroke ? `.overlay(RoundedRectangle(cornerRadius: ${r(rad?.[0] ?? 0)}).stroke(Color(hex: "#${hex8(stroke.color).rgb}"), lineWidth: ${r(stroke.width)}))` : "";
  const shadow = (n.effects ?? []).find((e) => e.visible !== false && e.type === "drop-shadow");
  const shadowMod = shadow ? `.shadow(color: Color(hex: "#${hex8(shadow.color ?? "#00000040").rgb}").opacity(${r(splitColor(shadow.color ?? "#00000040").alpha)}), radius: ${r(shadow.blur / 2)}, x: ${r(shadow.x ?? 0)}, y: ${r(shadow.y ?? 0)})` : "";
  const opacity = n.opacity !== undefined && n.opacity < 1 ? `.opacity(${r(n.opacity)})` : "";
  const mods = [strokeMod, shadowMod, opacity].filter(Boolean).map((m) => `\n${pad}    ${m}`).join("");
  switch (n.type) {
    case "text": {
      const f = n.font;
      const weight = f ? ({ 100: ".ultraLight", 200: ".thin", 300: ".light", 400: ".regular", 500: ".medium", 600: ".semibold", 700: ".bold", 800: ".heavy", 900: ".black" } as Record<number, string>)[f.weight] ?? ".regular" : ".regular";
      return `${pad}Text(${JSON.stringify(n.text ?? "")})\n${pad}    .font(.system(size: ${r(f?.size ?? 16)}, weight: ${weight}))\n${pad}    .foregroundColor(${color})${n.align === "center" ? `\n${pad}    .multilineTextAlignment(.center)` : ""}${mods}`;
    }
    case "ellipse":
      return `${pad}Ellipse()\n${pad}    .fill(${color})\n${pad}    ${frame}${mods}`;
    case "rect":
    case "image":
      return `${pad}RoundedRectangle(cornerRadius: ${r(rad?.[0] ?? 0)})\n${pad}    .fill(${color})\n${pad}    ${frame}${mods}`;
    default: {
      const kids = (n.children ?? []).filter((c) => c.visible !== false);
      const l = n.layout;
      const body = kids.map((c) => swiftui(c, depth + 1, n)).join("\n");
      if (l) {
        const stack = l.dir === "row" ? "HStack" : "VStack";
        const align = l.dir === "row" ? ({ start: ".top", center: ".center", end: ".bottom", stretch: ".center" } as const)[l.cross] : ({ start: ".leading", center: ".center", end: ".trailing", stretch: ".leading" } as const)[l.cross];
        const [pt, pr, pb, pl] = l.pad;
        const padding = pt || pr || pb || pl ? `\n${pad}    .padding(EdgeInsets(top: ${r(pt)}, leading: ${r(pl)}, bottom: ${r(pb)}, trailing: ${r(pr)}))` : "";
        return `${pad}${stack}(alignment: ${align}, spacing: ${r(l.gap)}) {\n${body}\n${pad}}${padding}\n${pad}${frame}\n${pad}    .background(${color})${rad ? `\n${pad}    .cornerRadius(${r(rad[0]!)})` : ""}${mods}`;
      }
      void parent;
      return `${pad}ZStack(alignment: .topLeading) {\n${kids.map((c) => `${swiftui(c, depth + 1, n)}\n${pad}    .offset(x: ${r(c.x)}, y: ${r(c.y)})`).join("\n")}\n${pad}}\n${pad}${frame}\n${pad}    .background(${color})${rad ? `\n${pad}    .cornerRadius(${r(rad[0]!)})` : ""}${mods}`;
    }
  }
}

function compose(n: DesignNode, depth = 0): string {
  const pad = "    ".repeat(depth);
  const size = sizeOf(n);
  const fill = (n.fills ?? []).find((f) => f.visible !== false);
  const color = fill?.type === "solid" ? `Color(0x${hex8(fill.color).a}${hex8(fill.color).rgb.toUpperCase()})` : "Color.Transparent";
  const rad = radiusOf(n);
  const shape = n.type === "ellipse" ? "CircleShape" : `RoundedCornerShape(${r(rad?.[0] ?? 0)}.dp)`;
  const base = `Modifier.size(${r(size.width)}.dp, ${r(size.height)}.dp)`;
  const stroke = (n.strokes ?? []).find((s) => s.visible !== false && s.width > 0);
  const mod = `${base}${n.opacity !== undefined && n.opacity < 1 ? `.alpha(${r(n.opacity)}f)` : ""}${fill ? `.background(${color}, ${shape})` : ""}${stroke ? `.border(${r(stroke.width)}.dp, Color(0x${hex8(stroke.color).a}${hex8(stroke.color).rgb.toUpperCase()}), ${shape})` : ""}`;
  switch (n.type) {
    case "text": {
      const f = n.font;
      return `${pad}Text(\n${pad}    text = ${JSON.stringify(n.text ?? "")},\n${pad}    color = ${color},\n${pad}    fontSize = ${r(f?.size ?? 16)}.sp,\n${pad}    fontWeight = FontWeight(${f?.weight ?? 400})\n${pad})`;
    }
    case "rect":
    case "ellipse":
    case "image":
      return `${pad}Box(modifier = ${mod})`;
    default: {
      const kids = (n.children ?? []).filter((c) => c.visible !== false);
      const l = n.layout;
      if (l) {
        const [pt, pr, pb, pl] = l.pad;
        const arr = `Arrangement.${l.main === "between" ? "SpaceBetween" : l.dir === "row" ? "spacedBy(" + r(l.gap) + ".dp)" : "spacedBy(" + r(l.gap) + ".dp)"}`;
        const comp = l.dir === "row" ? "Row" : "Column";
        const cross = l.dir === "row" ? ({ start: "Alignment.Top", center: "Alignment.CenterVertically", end: "Alignment.Bottom", stretch: "Alignment.Top" } as const)[l.cross] : ({ start: "Alignment.Start", center: "Alignment.CenterHorizontally", end: "Alignment.End", stretch: "Alignment.Start" } as const)[l.cross];
        const axis = l.dir === "row" ? "verticalAlignment" : "horizontalAlignment";
        const dir = l.dir === "row" ? "horizontalArrangement" : "verticalArrangement";
        return `${pad}${comp}(\n${pad}    modifier = ${mod}.padding(start = ${r(pl)}.dp, top = ${r(pt)}.dp, end = ${r(pr)}.dp, bottom = ${r(pb)}.dp),\n${pad}    ${dir} = ${arr},\n${pad}    ${axis} = ${cross}\n${pad}) {\n${kids.map((c) => compose(c, depth + 1)).join("\n")}\n${pad}}`;
      }
      return `${pad}Box(modifier = ${mod}) {\n${kids.map((c) => `${pad}    Box(modifier = Modifier.offset(${r(c.x)}.dp, ${r(c.y)}.dp)) {\n${compose(c, depth + 2)}\n${pad}    }`).join("\n")}\n${pad}}`;
    }
  }
}

/** Code for a layer in the chosen language. */
export function codeFor(n: DesignNode, kind: CodeKind, parent?: DesignNode | null): string {
  switch (kind) {
    case "css":
      return css(n, parent);
    case "tailwind":
      return tailwind(n, parent);
    case "swiftui":
      return swiftui(n, 0, parent);
    case "compose":
      return compose(n);
  }
}
