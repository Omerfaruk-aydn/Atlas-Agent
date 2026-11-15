// The design script API. A script calls these functions; they record design
// ops in __ops, which the Go side applies to the file in one undo step.
// __env comes from Go: {file, prefix, modes, mode, pages, vars}.
"use strict";

var __ops = [];
var __used = {};
var __system = { colors: {}, type: {}, space: [], radius: {}, shadows: {}, components: [], screens: [] };
var __placed = 0;

function __slug(s) {
  var out = String(s == null ? "" : s).toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-+|-+$/g, "");
  return out || "layer";
}

function __uid(base) {
  var stem = __env.prefix + ":" + __slug(base);
  if (stem.length > 58) stem = stem.slice(0, 58).replace(/-+$/, "");
  var id = stem;
  for (var i = 2; __used[id]; i++) id = stem + "-" + i;
  __used[id] = true;
  return id;
}

function __fail(msg) {
  throw new Error(msg);
}

function __isHex(s) {
  return typeof s === "string" && /^#([0-9a-fA-F]{3,4}|[0-9a-fA-F]{6}|[0-9a-fA-F]{8})$/.test(s);
}

// A colour reference: a token defined with colors(), an existing variable
// id (c-text) or a #hex. Returns {color, variable}.
function __color(ref) {
  if (__isHex(ref)) return { color: ref.toUpperCase(), variable: "" };
  var tok = __system.colors[ref];
  if (tok) return { color: tok.value, variable: tok.id };
  var known = __env.vars[ref] || __env.vars["c-" + ref];
  if (known) return { color: known.value, variable: known.id };
  __fail("unknown colour " + JSON.stringify(ref) + ": define it with colors({...}) first, or use #RRGGBB");
}

function __paints(v) {
  if (v === undefined) return undefined;
  if (v === null || v === "none") return [];
  if (Array.isArray(v)) {
    var out = [];
    for (var i = 0; i < v.length; i++) out = out.concat(__paints(v[i]));
    return out;
  }
  if (typeof v === "object") {
    if (v.type === "linear" || v.type === "radial" || v.type === "angular") {
      var stops = (v.stops || []).map(function (s, i, all) {
        if (typeof s === "string") return { offset: all.length > 1 ? i / (all.length - 1) : 0, color: __color(s).color };
        return { offset: s.offset, color: __color(s.color).color };
      });
      var p = { type: v.type, stops: stops };
      if (v.angle !== undefined) p.angle = v.angle;
      return [p];
    }
    if (v.type === "image") return [v];
    var c = __color(v.color);
    var paint = { type: "solid", color: c.color };
    if (c.variable) paint.variable = c.variable;
    if (v.opacity !== undefined) paint.opacity = v.opacity;
    return [paint];
  }
  var r = __color(v);
  var solid = { type: "solid", color: r.color };
  if (r.variable) solid.variable = r.variable;
  return [solid];
}

function __strokes(v) {
  if (v === undefined) return undefined;
  if (v === null || v === "none") return [];
  var list = Array.isArray(v) ? v : [v];
  return list.map(function (s) {
    if (typeof s === "string") s = { color: s };
    var c = __color(s.color);
    var out = { color: c.color, width: s.width === undefined ? 1 : s.width, align: s.align || "inside" };
    if (c.variable) out.variable = c.variable;
    if (s.dash) out.dash = s.dash;
    return out;
  });
}

function __radius(v) {
  if (v === undefined) return undefined;
  if (typeof v === "string") {
    if (__system.radius[v] === undefined) __fail("unknown radius " + JSON.stringify(v) + ": define it with radius({...}) first");
    return __system.radius[v];
  }
  return v;
}

function __pad(v) {
  if (v === undefined) return [0, 0, 0, 0];
  if (typeof v === "number") return [v, v, v, v];
  if (v.length === 2) return [v[0], v[1], v[0], v[1]];
  if (v.length === 4) return v;
  __fail("pad is a number, [vertical, horizontal] or [top, right, bottom, left]");
}

var __layoutKeys = { dir: 1, gap: 1, pad: 1, main: 1, cross: 1 };
var __handled = { name: 1, w: 1, h: 1, fill: 1, stroke: 1, radius: 1, shadow: 1, children: 1, type: 1, id: 1 };

// __node turns friendly props into a design node.
function __node(type, props, children) {
  props = props || {};
  var n = { type: type, name: props.name || type };
  n.id = __uid(props.id || props.name || type);
  if (typeof props.w === "number") n.width = props.w;
  else if (props.w === "fill" || props.w === "hug") n.sizing_x = props.w;
  if (typeof props.h === "number") n.height = props.h;
  else if (props.h === "fill" || props.h === "hug") n.sizing_y = props.h;
  // Frames are see-through unless given a fill, like a stack in a design tool.
  var fills = __paints(props.fill === undefined && (type === "frame" || type === "component") ? [] : props.fill);
  if (fills !== undefined) n.fills = fills;
  var strokes = __strokes(props.stroke);
  if (strokes !== undefined) n.strokes = strokes;
  var radius = __radius(props.radius);
  if (radius !== undefined) n.radius = radius;
  if (props.shadow) {
    var sh = __system.shadows[props.shadow];
    if (!sh) __fail("unknown shadow " + JSON.stringify(props.shadow) + ": define it with shadow(name, ...) first");
    n.effect_style = sh.id;
    n.effects = sh.effects;
  }
  var layout = null;
  for (var k in props) {
    if (__handled[k]) continue;
    if (__layoutKeys[k]) {
      layout = layout || {};
      layout[k] = k === "pad" ? __pad(props[k]) : props[k];
      continue;
    }
    n[k] = props[k];
  }
  if (layout) {
    n.layout = { dir: layout.dir || "column", gap: layout.gap || 0, pad: __pad(layout.pad), main: layout.main || "start", cross: layout.cross || "start" };
    if (props.w === undefined) n.sizing_x = "hug";
    if (props.h === undefined) n.sizing_y = "hug";
  }
  if (children !== undefined) {
    n.children = (Array.isArray(children) ? children : [children]).filter(function (c) {
      return c !== null && c !== undefined && c !== false;
    });
  }
  return n;
}

// ---- Tokens ----

// colors({"bg": ["#F7F5F0", "#0E1113"], "brand": "#0F5C4A"}): colour
// variables. A pair is [light, dark]; a single value serves every mode.
function colors(map) {
  var pairs = false;
  for (var k in map) if (Array.isArray(map[k]) || (map[k] && typeof map[k] === "object")) pairs = true;
  var modes = __env.modes.slice();
  if (pairs && (modes.indexOf("Light") < 0 || modes.indexOf("Dark") < 0)) {
    modes = modes.filter(function (m) { return m !== "Default" && m !== "Light" && m !== "Dark"; });
    modes = ["Light", "Dark"].concat(modes);
    __ops.push({ op: "set_modes", modes: modes, mode: "Light" });
    __env.modes = modes;
    __env.mode = "Light";
  }
  if (__env.modes.length === 0) {
    __ops.push({ op: "set_modes", modes: ["Light"], mode: "Light" });
    __env.modes = ["Light"];
    __env.mode = "Light";
  }
  var out = {};
  for (var name in map) {
    var v = map[name];
    var values = {};
    if (Array.isArray(v)) {
      if (!__isHex(v[0]) || !__isHex(v[1])) __fail("colour " + name + " needs [light, dark] as #RRGGBB");
      for (var i = 0; i < __env.modes.length; i++) values[__env.modes[i]] = __env.modes[i] === "Dark" ? v[1] : v[0];
    } else if (v && typeof v === "object") {
      for (var m in v) values[m] = v[m];
    } else {
      if (!__isHex(v)) __fail("colour " + name + " is not #RRGGBB");
      for (var j = 0; j < __env.modes.length; j++) values[__env.modes[j]] = v;
    }
    var id = "c-" + __slug(name);
    __ops.push({ op: "set_variable", id: id, name: name, props: { values: values } });
    var tok = { id: id, name: name, value: (values[__env.mode] || values[__env.modes[0]]).toUpperCase(), values: values };
    __system.colors[name] = tok;
    out[name] = name;
  }
  return out;
}

// type({family: "Inter", styles: {display: {size: 34, weight: 700, line: 40, tracking: -0.6}}})
function type(spec) {
  var family = spec.family || "Inter";
  var styles = spec.styles || spec;
  for (var name in styles) {
    if (name === "family") continue;
    var s = styles[name];
    if (!s.size) __fail("type style " + name + " needs size");
    var font = { family: s.family || family, size: s.size, weight: s.weight || 400 };
    if (s.italic) font.style = "italic";
    var props = { font: font };
    props.line_height = s.line || Math.round(s.size * 1.3);
    if (s.tracking !== undefined) props.letter_spacing = s.tracking;
    if (s.transform) props.transform = { uppercase: "upper", lowercase: "lower" }[s.transform] || s.transform;
    var id = "t-" + __slug(name);
    __ops.push({ op: "set_style", kind: "text", id: id, name: name, props: props });
    __system.type[name] = { id: id, font: font, line_height: props.line_height, letter_spacing: props.letter_spacing, transform: props.transform };
  }
}

// space([4, 8, 12, 16, 24, 32, 48]): the spacing scale lint holds gaps and
// padding to.
function space(list) {
  __system.space = list.slice().sort(function (a, b) { return a - b; });
  return __system.space;
}

// radius({sm: 8, md: 12, lg: 20, full: 999})
function radius(map) {
  for (var k in map) __system.radius[k] = map[k];
  return map;
}

// shadow("raised", {y: 2, blur: 8, color: "#0000001F"}) or a list of them.
function shadow(name, list) {
  list = Array.isArray(list) ? list : [list];
  var effects = list.map(function (e) {
    var out = { type: e.inner ? "inner-shadow" : "drop-shadow", x: e.x || 0, y: e.y || 0, blur: e.blur || 0, spread: e.spread || 0, color: e.color || "#0000001F" };
    return out;
  });
  var id = "e-" + __slug(name);
  __ops.push({ op: "set_style", kind: "effect", id: id, name: name, props: { effects: effects } });
  __system.shadows[name] = { id: id, effects: effects };
}

// ---- Layers ----

function frame(props, children) { return __node("frame", props, children || []); }

function row(props, children) {
  props = props || {};
  var p = {};
  for (var k in props) p[k] = props[k];
  p.dir = "row";
  if (p.cross === undefined) p.cross = "center";
  return __node("frame", p, children || []);
}

function col(props, children) {
  props = props || {};
  var p = {};
  for (var k in props) p[k] = props[k];
  p.dir = "column";
  return __node("frame", p, children || []);
}

function rect(props) { return __node("rect", props); }
function ellipse(props) { return __node("ellipse", props); }

function image(src, props) {
  props = props || {};
  var n = __node("image", props);
  n.src = src;
  if (!n.fit) n.fit = "fill";
  return n;
}

// text("48 min to start", "display", {color: "text"}): a text layer in one
// of the type() styles.
function text(str, style, props) {
  props = props || {};
  var st = __system.type[style];
  if (!st) __fail("unknown text style " + JSON.stringify(style) + ": define it with type({...}) first");
  var p = {};
  for (var k in props) if (k !== "color") p[k] = props[k];
  if (p.name === undefined) p.name = String(str).slice(0, 40);
  var n = __node("text", p);
  n.text = String(str);
  n.text_style = st.id;
  n.font = { family: st.font.family, size: st.font.size, weight: st.font.weight };
  if (st.font.style) n.font.style = st.font.style;
  n.line_height = st.line_height;
  if (st.letter_spacing !== undefined) n.letter_spacing = st.letter_spacing;
  if (st.transform) n.transform = st.transform;
  n.fills = __paints(props.color || (__system.colors.text ? "text" : "#111111"));
  if (props.w === "fill" || typeof props.w === "number") n.auto_resize = n.auto_resize || "height";
  return n;
}

// icon("calendar", {size: 20, color: "text-muted"}): a Lucide icon.
function icon(name, props) {
  props = props || {};
  var size = props.size || 24;
  var c = __color(props.color || (__system.colors.text ? "text" : "#111111"));
  var w = Math.max(1.25, Math.round((2 * size / 24) * 100) / 100);
  var stroke = { color: c.color, width: props.weight || w, cap: "round", join: "round" };
  if (c.variable) stroke.variable = c.variable;
  var n = { type: "path", icon: name, width: size, height: size, fills: [], strokes: [stroke], name: props.name || "Icon/" + name };
  n.id = __uid(props.id || "icon-" + name);
  __place(props, n);
  if (props.rotation) n.rotation = props.rotation;
  return n;
}

// component("Button", {...frame props}, children): a main component.
function component(name, props, children) {
  props = props || {};
  var p = {};
  for (var k in props) p[k] = props[k];
  p.name = name;
  var n = __node("component", p, children || []);
  if (n.fills === undefined) n.fills = [];
  __system.components.push({ id: n.id, name: name });
  return n;
}

// variants("Button", {state: ["default", "pressed", "disabled"], size: ["md", "sm"]},
//   function (v) { return component(..., ...); })
// makes every combination as a component of the set, laid out in a frame.
function variants(name, axes, build) {
  var keys = Object.keys(axes);
  if (keys.length === 0) __fail("variants " + name + " needs at least one property");
  var combos = [{}];
  keys.forEach(function (k) {
    var next = [];
    combos.forEach(function (c) {
      axes[k].forEach(function (v) {
        var o = {};
        for (var x in c) o[x] = c[x];
        o[k] = String(v);
        next.push(o);
      });
    });
    combos = next;
  });
  var made = [];
  var rows = {};
  var order = [];
  combos.forEach(function (v) {
    var n = build(v);
    if (!n || (n.type !== "component" && n.type !== "frame")) __fail("variants " + name + ": build must return component(...) or frame(...)");
    if (n.type === "frame") {
      n.type = "component";
      if (n.fills === undefined) n.fills = [];
      __system.components.push({ id: n.id, name: name });
    } else {
      __system.components[__system.components.length - 1].name = name;
    }
    n.set = name;
    n.variant = v;
    n.name = keys.map(function (k) { return k + "=" + v[k]; }).join(", ");
    made.push({ variant: v, node: n });
    var rowKey = v[keys[0]];
    if (!rows[rowKey]) { rows[rowKey] = []; order.push(rowKey); }
    rows[rowKey].push(n);
  });
  var holder = __node("frame", { name: name, dir: "column", gap: __near(24), pad: __near(32), fill: [], stroke: { color: "#9747FF", width: 1, dash: [6, 4] }, radius: __nearRadius(8) },
    order.map(function (key) { return __node("frame", { name: keys[0] + "=" + key, dir: "row", gap: __near(24), cross: "start", fill: [] }, rows[key]); }));
  return { kind: "set", name: name, axes: axes, keys: keys, items: made, node: holder };
}

function __findByName(n, name) {
  if (n.name === name) return n;
  var kids = n.children || [];
  for (var i = 0; i < kids.length; i++) {
    var hit = __findByName(kids[i], name);
    if (hit) return hit;
  }
  return null;
}

// instance(button, {variant: {state: "default"}, text: {Label: "Book a seat"}, hide: ["Icon"]})
function instance(ref, opts) {
  opts = opts || {};
  var master = ref;
  if (ref && ref.kind === "set") {
    var want = opts.variant || {};
    master = null;
    for (var i = 0; i < ref.items.length && !master; i++) {
      var ok = true;
      for (var k in want) if (ref.items[i].variant[k] !== String(want[k])) ok = false;
      if (ok) master = ref.items[i].node;
    }
    if (!master) __fail("no variant of " + ref.name + " matches " + JSON.stringify(want));
  }
  if (!master || master.type !== "component") __fail("instance needs a component(...) or variants(...) result");
  var n = { type: "instance", component_id: master.id, name: opts.name || (ref.kind === "set" ? ref.name : master.name) };
  n.id = __uid(opts.id || n.name);
  if (master.variant) n.variant = master.variant;
  // The instance's own box draws like its main's: fills, strokes, corners,
  // shadows and clipping come along, as when one is placed in the editor.
  ["fills", "strokes", "radius", "effects", "effect_style", "clip", "opacity"].forEach(function (key) {
    if (master[key] !== undefined) n[key] = JSON.parse(JSON.stringify(master[key]));
  });
  var over = {};
  var t = opts.text || {};
  for (var layer in t) {
    var hit = __findByName(master, layer);
    if (!hit || hit.type !== "text") __fail("instance of " + master.name + ": no text layer named " + JSON.stringify(layer));
    over[hit.id] = over[hit.id] || {};
    over[hit.id].text = String(t[layer]);
  }
  (opts.hide || []).forEach(function (layer) {
    var hit = __findByName(master, layer);
    if (!hit) __fail("instance of " + master.name + ": no layer named " + JSON.stringify(layer));
    over[hit.id] = over[hit.id] || {};
    over[hit.id].visible = false;
  });
  var icons = opts.icons || {};
  for (var il in icons) {
    var ih = __findByName(master, il);
    if (!ih || ih.type !== "path") __fail("instance of " + master.name + ": no icon layer named " + JSON.stringify(il));
    over[ih.id] = over[ih.id] || {};
    over[ih.id].d = __iconPath(icons[il]);
  }
  var fills = opts.fills || {};
  for (var fl in fills) {
    var fh = __findByName(master, fl);
    if (!fh) __fail("instance of " + master.name + ": no layer named " + JSON.stringify(fl));
    over[fh.id] = over[fh.id] || {};
    over[fh.id].fills = __paints(fills[fl]);
  }
  if (Object.keys(over).length) n.overrides = over;
  // fill recolours the instance's own box: one Tile component, a tile per hue.
  if (opts.fill !== undefined) n.fills = __paints(opts.fill);
  if (typeof opts.w === "number") n.width = opts.w;
  else if (opts.w === "fill") n.sizing_x = "fill";
  if (typeof opts.h === "number") n.height = opts.h;
  else if (opts.h === "fill") n.sizing_y = "fill";
  var own = { variant: 1, text: 1, hide: 1, fills: 1, fill: 1, icons: 1, w: 1, h: 1, name: 1, id: 1 };
  for (var key in opts) if (!own[key]) n[key] = opts[key];
  return n;
}

// ---- Pages and placing ----

function page(name, opts) {
  opts = opts || {};
  if (__env.pages.indexOf(name) < 0) {
    __ops.push({ op: "add_page", name: name, background: opts.background || "#F5F5F5" });
    __env.pages.push(name);
  } else if (opts.background) {
    __ops.push({ op: "update_page", page: name, background: opts.background });
  }
  return name;
}

// place(node, {page: "Library", x: 0, y: 0}) puts a layer on a page.
function place(node, opts) {
  opts = opts || {};
  if (node && node.kind === "set") node = node.node;
  if (!node || !node.type) __fail("place needs a layer");
  if (opts.x !== undefined) node.x = opts.x;
  if (opts.y !== undefined) node.y = opts.y;
  if (opts.page && __env.pages.indexOf(opts.page) < 0) page(opts.page);
  var op = { op: "add", node: node, page: opts.page || __env.pages[0] };
  if (opts.parent) op.parent = typeof opts.parent === "string" ? opts.parent : opts.parent.id;
  __ops.push(op);
  __placed++;
  return node;
}

var __devices = { iphone: [393, 852], "iphone-se": [375, 667], "iphone-pro-max": [430, 932], android: [412, 915], tablet: [834, 1194], desktop: [1440, 1024], laptop: [1280, 800] };

// screen("01 Today", {device: "iphone", fill: "bg", pad: [0, 16], gap: 16}, children)
function screen(name, props, children) {
  props = props || {};
  var size = __devices[props.device || "iphone"];
  if (!size) __fail("unknown device " + props.device + ": use " + Object.keys(__devices).join(", "));
  var p = {};
  for (var k in props) if (k !== "device") p[k] = props[k];
  p.name = name;
  p.w = size[0];
  // h makes a scrolling screen longer than the device (a home feed).
  p.h = typeof props.h === "number" && props.h > size[1] ? props.h : size[1];
  if (p.dir === undefined && p.gap === undefined && p.pad === undefined) p.dir = "column";
  if (p.fill === undefined) p.fill = __system.colors.bg ? "bg" : "#FFFFFF";
  if (p.grids === undefined) {
    p.grids = size[0] < 600 ? [{ pattern: "columns", count: 4, gutter: 16, margin: 16, align: "stretch", color: "#FF000014" }]
      : [{ pattern: "columns", count: 12, gutter: 24, margin: size[0] >= 1280 ? 80 : 32, align: "stretch", color: "#FF000014" }];
  }
  var n = __node("frame", p, children || []);
  n.clip = true;
  __system.screens.push({ id: n.id, name: name });
  return n;
}

// library([button, field, tabBar], {page: "Library"}) lays components and
// sets out in a column on a page of their own, each under a label.
function library(items, opts) {
  opts = opts || {};
  var pg = page(opts.page || "Library", { background: opts.background || "#F4F4F5" });
  var nodes = items.map(function (it) { return it && it.kind === "set" ? it.node : it; });
  // The board is filled with the bg token, so components are seen (and
  // checked) on the app's own background in every mode.
  var board = __node("frame", { name: opts.name || "Components", dir: "column", gap: opts.gap || __near(80), pad: __near(64), radius: __nearRadius(24), fill: __system.colors.bg ? "bg" : [] }, nodes);
  place(board, { page: pg, x: opts.x || 0, y: opts.y || 0 });
  return pg;
}

// __near is the value of the spacing scale closest to v, so the frames the
// API makes for you sit on the file's own scale.
function __near(v) {
  var s = __system.space;
  if (!s.length) return v;
  var best = s[0];
  for (var i = 1; i < s.length; i++) if (Math.abs(s[i] - v) < Math.abs(best - v)) best = s[i];
  return best;
}

function __nearRadius(v) {
  var best = null;
  for (var k in __system.radius) {
    var r = __system.radius[k];
    if (typeof r === "number" && r < 100 && (best === null || Math.abs(r - v) < Math.abs(best - v))) best = r;
  }
  return best === null ? v : best;
}

// ---- Material: maps, data marks, textures, vectors ----
// A screen of only rectangles and text is a wireframe. These draw the
// subject's own things as real vectors, in the file's colour tokens.

function __num(v) {
  return Math.round(v * 10) / 10;
}

// __rng is a small seeded generator, so the same seed draws the same map.
function __rng(seed) {
  var a = 0;
  var s = String(seed === undefined ? "atlas" : seed);
  for (var i = 0; i < s.length; i++) a = (a * 31 + s.charCodeAt(i)) | 0;
  return function () {
    a = (a + 0x6d2b79f5) | 0;
    var t = Math.imul(a ^ (a >>> 15), 1 | a);
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

// __tok picks the colour given, else the first fallback the file defines.
function __tok(ref, fallback) {
  if (ref !== undefined) return ref;
  for (var i = 0; i < fallback.length; i++) {
    var f = fallback[i];
    if (__isHex(f) || __system.colors[f] || __env.vars[f] || __env.vars["c-" + f]) return f;
  }
  return "#888888";
}

function __line(ref, width, extra) {
  var c = __color(ref);
  var s = { color: c.color, width: width, cap: "round", join: "round" };
  if (c.variable) s.variable = c.variable;
  for (var k in extra || {}) s[k] = extra[k];
  return [s];
}

// path("M0 0 L10 10", {w: 120, h: 40, view: [120, 40], fill: "brand"}): a
// vector. A stroke takes a colour and a weight: {stroke: "line", weight: 2}.
function path(d, props) {
  props = props || {};
  if (typeof props.w !== "number" || typeof props.h !== "number") __fail("path needs w and h in pixels");
  var p = {};
  for (var k in props) if (k !== "view" && k !== "weight" && k !== "stroke" && k !== "dash" && props[k] !== undefined) p[k] = props[k];
  if (p.fill === undefined) p.fill = null;
  if (!p.name) p.name = "Vector";
  var n = __node("path", p);
  n.d = d;
  n.view_box = props.view || [props.w, props.h];
  if (props.stroke) n.strokes = __line(props.stroke, props.weight || 1.5, props.dash ? { dash: props.dash } : null);
  return n;
}

function __poly(points, w, h, close) {
  var d = "";
  for (var i = 0; i < points.length; i++) d += (i ? " L" : "M") + __num(points[i][0] * w) + " " + __num(points[i][1] * h);
  return close ? d + " Z" : d;
}

// __smooth draws a curve through points (0-1 units) with Catmull-Rom.
function __smooth(points, w, h) {
  var P = points.map(function (p) { return [p[0] * w, p[1] * h]; });
  if (P.length < 3) return __poly(points, w, h, false);
  var d = "M" + __num(P[0][0]) + " " + __num(P[0][1]);
  for (var i = 0; i < P.length - 1; i++) {
    var p0 = P[i - 1] || P[i], p1 = P[i], p2 = P[i + 1], p3 = P[i + 2] || P[i + 1];
    d += " C" + __num(p1[0] + (p2[0] - p0[0]) / 6) + " " + __num(p1[1] + (p2[1] - p0[1]) / 6) + " " +
      __num(p2[0] - (p3[0] - p1[0]) / 6) + " " + __num(p2[1] - (p3[1] - p1[1]) / 6) + " " + __num(p2[0]) + " " + __num(p2[1]);
  }
  return d;
}

function __place(props, into) {
  for (var key in props) if (/^(x|y|absolute|sizing_x|sizing_y|opacity)$/.test(key) && props[key] !== undefined) into[key] = props[key];
  return into;
}

// map({w: 361, h: 200, seed: "blue-zone", pins: [{x: .3, y: .6, kind: "you"},
// {x: .72, y: .35, label: "Hall B2"}, {x: .5, y: .4, kind: "tag", label:
// "350 kW", selected: true}], route: true, labelStyle: "caption"}):
// a quiet street-map crop drawn from the seed: land, a river, parks, a
// street grid, two main roads, the route between the first and last pin and
// the pins. Positions are shares of the box (0-1). Colours come from the
// map-land, map-water, map-park, map-road and brand tokens when the file
// defines them, else from muted defaults; pass land, water, park, road,
// minor, accent, ink or halo to choose.
function map(props) {
  props = props || {};
  var w = props.w || 361, h = props.h || 200;
  var rnd = __rng(props.seed);
  var land = __tok(props.land, ["map-land", "surface-2", "#EEF0EA"]);
  var water = __tok(props.water, ["map-water", "#CFE0E8"]);
  var park = __tok(props.park, ["map-park", "#DCE6D3"]);
  var road = __tok(props.road, ["map-road", "surface", "#FFFFFF"]);
  var minor = __tok(props.minor, ["map-minor", "line", "#E2E4DE"]);
  var accent = __tok(props.accent, ["brand", "accent", "#0F5C4A"]);
  var ink = __tok(props.ink, ["text", "#111111"]);
  var halo = __tok(props.halo, ["surface", "bg", "#FFFFFF"]);
  var layers = [rect({ name: "Land", w: w, h: h, fill: land })];
  if (props.river !== false) {
    var y0 = 0.15 + rnd() * 0.7, pts = [];
    for (var i = 0; i <= 6; i++) pts.push([i / 6 * 1.1 - 0.05, Math.min(0.95, Math.max(0.05, y0 + Math.sin(i * 1.1 + rnd() * 2) * 0.12))]);
    layers.push(path(__smooth(pts, w, h), { name: "Water", w: w, h: h, stroke: water, weight: Math.max(10, Math.round(h * 0.09)) }));
  }
  for (var k = 0, nParks = props.parks === undefined ? 2 : props.parks; k < nParks; k++) {
    var pw = w * (0.12 + rnd() * 0.14), ph = h * (0.14 + rnd() * 0.16);
    layers.push(rect({ name: "Park", x: __num(rnd() * (w - pw)), y: __num(rnd() * (h - ph)), w: __num(pw), h: __num(ph), radius: __nearRadius(4), fill: park }));
  }
  var step = props.block || 28, tilt = (rnd() - 0.5) * 0.35, grid = "";
  for (var gx = -h; gx < w + h; gx += step * (0.8 + rnd() * 0.5)) grid += "M" + __num(gx) + " 0 L" + __num(gx + h * tilt) + " " + h + " ";
  for (var gy = 0; gy < h + step * 2; gy += step * (0.8 + rnd() * 0.5)) grid += "M0 " + __num(gy) + " L" + w + " " + __num(gy - w * tilt * 0.6) + " ";
  layers.push(path(grid.trim(), { name: "Streets", w: w, h: h, stroke: minor, weight: 1.25 }));
  var mains = "";
  for (var m = 0; m < 2; m++) {
    // A main road wanders a little around one line; it never doubles back.
    var at = 0.2 + rnd() * 0.6, a = [];
    for (var q = 0; q < 4; q++) a.push([q / 3 * 1.1 - 0.05, Math.min(0.95, Math.max(0.05, at + (rnd() - 0.5) * 0.24))]);
    if (m === 1) a = a.map(function (p) { return [p[1], p[0]]; });
    mains += __smooth(a, w, h) + " ";
  }
  layers.push(path(mains.trim(), { name: "Roads", w: w, h: h, stroke: road, weight: 5 }));
  var pins = props.pins || [];
  var route = props.route;
  if (route === true && pins.length >= 2) {
    var s = pins[0], e = pins[pins.length - 1];
    route = [[s.x, s.y], [s.x + (e.x - s.x) * 0.45, s.y], [s.x + (e.x - s.x) * 0.45, e.y], [e.x, e.y]];
  }
  if (route && route.length) {
    layers.push(path(__poly(route, w, h, false), { name: "Route casing", w: w, h: h, stroke: halo, weight: 8 }));
    layers.push(path(__poly(route, w, h, false), { name: "Route", w: w, h: h, stroke: accent, weight: 4 }));
  }
  pins.forEach(function (pin) {
    var px = pin.x * w, py = pin.y * h;
    if (pin.kind === "tag") {
      // A callout: the label on an accent bubble with a stem down to a dot,
      // for prices, power or counts on the map. selected adds a halo.
      var tst = __system.type[pin.style || props.labelStyle || __labelStyle(props)];
      var tsz = tst ? tst.font.size : 13, tlh = tst ? tst.line_height : 16;
      var tw = Math.ceil(__textWidth(String(pin.label), tst ? tst.font.family : "Inter", tst ? tst.font.weight : 600, tsz)) + 20, th = tlh + 10;
      var tx0 = Math.min(w - tw - 2, Math.max(2, px - tw / 2)), ty0 = py - th - 12;
      var tagFill = pin.color || accent;
      if (pin.selected) layers.push(ellipse({ name: "Tag/halo", x: __num(px - 18), y: __num(py - 18), w: 36, h: 36, fill: { color: tagFill, opacity: 0.22 }, stroke: { color: tagFill, width: 2 } }));
      layers.push(path("M0 0 L12 0 L6 10 Z", { name: "Tag/stem", w: 12, h: 10, x: __num(px - 6), y: __num(ty0 + th - 1), fill: tagFill }));
      layers.push(ellipse({ name: "Tag/dot", x: __num(px - 4), y: __num(py - 4), w: 8, h: 8, fill: tagFill, stroke: { color: halo, width: 2, align: "outside" } }));
      layers.push(row({ name: "Tag", x: __num(tx0), y: __num(ty0), w: __num(tw), h: th, radius: "full", fill: tagFill, main: "center", cross: "center" },
        [text(String(pin.label), pin.style || props.labelStyle || __labelStyle(props), { color: pin.ink || __tok(undefined, ["on-brand", "#FFFFFF"]) })]));
      return;
    }
    if (pin.kind === "you") {
      layers.push(ellipse({ name: "You/halo", x: __num(px - 18), y: __num(py - 18), w: 36, h: 36, fill: { color: accent, opacity: 0.18 } }));
      layers.push(ellipse({ name: "You", x: __num(px - 8), y: __num(py - 8), w: 16, h: 16, fill: accent, stroke: { color: halo, width: 3, align: "outside" } }));
    } else {
      layers.push(ellipse({ name: "Pin", x: __num(px - 7), y: __num(py - 7), w: 14, h: 14, fill: ink, stroke: { color: halo, width: 3, align: "outside" } }));
    }
    if (pin.label && props.labelStyle) {
      // The label sits above the pin, centred, kept inside the box.
      var st = __system.type[props.labelStyle];
      var size = st ? st.font.size : 13, lh = st ? st.line_height : 16;
      var padY = __near(4), padX = __near(8);
      var lw = Math.ceil(__textWidth(String(pin.label), st ? st.font.family : "Inter", st ? st.font.weight : 500, size)) + padX * 2;
      var lhgt = lh + padY * 2;
      var lx = Math.min(w - lw - 4, Math.max(4, px - lw / 2));
      var ly = py - 12 - lhgt < 4 ? py + 14 : py - 12 - lhgt;
      var t = text(pin.label, props.labelStyle, { color: ink });
      layers.push(row({ name: "Pin label", x: __num(lx), y: __num(ly), pad: [padY, padX], radius: __nearRadius(4), fill: halo, shadow: props.labelShadow }, [t]));
    }
  });
  var root = frame(__place(props, { name: props.name || "Map", w: w, h: h, radius: props.radius === undefined ? __nearRadius(16) : props.radius }), layers);
  root.clip = true;
  return root;
}

// chart(kind, data, props): a real data mark in the file's tokens.
// spark: a smooth line with a soft area and a dot on the latest value.
// area: a filled curve fading to nothing, with grid lines (grid: n) and an
//   optional glow, for a balance or a trend that is the screen's hero.
// bars: one bar per value; highlight (index, default the last, or "all")
//   in the accent, the rest in track. labels: a label under each bar;
//   values: a label on each bar; behind: true draws a full-height track
//   behind every bar.
// ring: data is a share 0-1 drawn as a progress ring, w across. sweep
//   (degrees, e.g. 270) makes it a gauge open at the bottom; dot: a colour
//   puts a dot where the progress ends (a timer's hand); glow lights it.
// wave: data is a list of amplitudes 0-1 (or a count), an audio waveform.
// zones: data is a list of colours, one segment each; value 0-1 places a
//   marker; labels name the segments; widths gives each segment's share
//   (sleep stages, a budget split); bar sets the height; each: true rounds
//   every segment, not only the two ends.
function __hexAlpha(ref, alpha) {
  var hex = __color(ref).color.slice(0, 7);
  var a = Math.round(Math.max(0, Math.min(1, alpha)) * 255).toString(16).toUpperCase();
  return hex + (a.length < 2 ? "0" + a : a);
}

function __labelStyle(props) {
  var st = props.labelStyle || (__system.type.caption ? "caption" : Object.keys(__system.type)[0]);
  if (!st) __fail("chart labels need a text style: define type({...}) first or pass labelStyle");
  return st;
}

function chart(kind, data, props) {
  props = props || {};
  var w = props.w || 120, h = props.h || (kind === "ring" ? w : 40);
  var accent = __tok(props.color, ["brand", "accent", "#0F5C4A"]);
  var track = __tok(props.track, ["surface-2", "line", "#E5E7EB"]);
  var ink = __tok(props.ink, ["text-muted", "text", "#666666"]);
  var name = props.name || "Chart/" + kind;
  if (kind === "ring" && (props.sweep || props.dot)) {
    // A gauge: an arc of sweep degrees (centred on 12 o'clock unless start
    // is given), the progress over it and an optional dot at its end.
    var gs = Math.max(0, Math.min(1, Array.isArray(data) ? data[0] : data));
    var sweep = Math.min(360, props.sweep || 360), a0 = props.start === undefined ? (sweep < 360 ? -sweep / 2 : 0) : props.start;
    var gw = props.weight || Math.max(3, Math.round(w / 12)), dotR = props.dot ? Math.max(gw * 1.6, w / 28) : 0;
    var gr = (w - Math.max(gw, dotR * 2)) / 2, gc = w / 2;
    var gk = [path(__arc(gc, gc, gr, a0, a0 + sweep), { name: "Track", w: w, h: w, stroke: track, weight: gw })];
    if (gs > 0 && accent !== track) gk.push(path(__arc(gc, gc, gr, a0, a0 + sweep * gs), { name: "Progress", w: w, h: w, stroke: accent, weight: gw }));
    if (gk.length > 1 && props.glow) gk[1].effects = [{ type: "drop-shadow", x: 0, y: 0, blur: 10, color: __hexAlpha(accent, 0.6) }];
    if (props.dot) {
      var dp = __pt(gc, gc, gr, a0 + sweep * gs);
      gk.push(ellipse({ name: "Dot", x: __num(dp[0] - dotR), y: __num(dp[1] - dotR), w: __num(dotR * 2), h: __num(dotR * 2), fill: __tok(props.dot, ["danger", "#E5484D"]) }));
    }
    return frame(__place(props, { name: name, w: w, h: w }), gk);
  }
  if (kind === "ring") {
    var share = Math.max(0, Math.min(1, Array.isArray(data) ? data[0] : data));
    var sw = props.weight || Math.max(3, Math.round(w / 12)), r = (w - sw) / 2, c = w / 2;
    var ang = share * Math.PI * 2 - Math.PI / 2;
    var ex = c + r * Math.cos(ang), ey = c + r * Math.sin(ang);
    var arc = share >= 0.999 ? "M" + c + " " + __num(c - r) + " A" + __num(r) + " " + __num(r) + " 0 1 1 " + __num(c - 0.01) + " " + __num(c - r)
      : "M" + c + " " + __num(c - r) + " A" + __num(r) + " " + __num(r) + " 0 " + (share > 0.5 ? 1 : 0) + " 1 " + __num(ex) + " " + __num(ey);
    var ring = [ellipse({ name: "Track", x: __num(sw / 2), y: __num(sw / 2), w: __num(w - sw), h: __num(w - sw), fill: null, stroke: { color: track, width: sw, align: "center" } })];
    if (share > 0) ring.push(path(arc, { name: "Progress", w: w, h: w, stroke: accent, weight: sw }));
    return frame(__place(props, { name: name, w: w, h: w }), ring);
  }
  if (kind === "wave") {
    var amps = Array.isArray(data) ? data.map(Number) : [];
    if (!amps.length) {
      var rnd = __rng(props.seed || "wave"), count = Number(data) || 48;
      for (var wi = 0; wi < count; wi++) amps.push(0.15 + 0.85 * Math.abs(Math.sin(wi * 0.55) * 0.6 + (rnd() - 0.5) * 0.8));
    }
    var wgap = props.gap === undefined ? 2 : props.gap, ww = Math.max(1, (w - wgap * (amps.length - 1)) / amps.length);
    var d = "";
    amps.forEach(function (a, i) {
      var bh = Math.max(ww, Math.min(1, a) * h), x = i * (ww + wgap), y = (h - bh) / 2, rr = ww / 2;
      d += "M" + __num(x) + " " + __num(y + rr) + " a" + __num(rr) + " " + __num(rr) + " 0 0 1 " + __num(ww) + " 0 v" + __num(bh - ww) +
        " a" + __num(rr) + " " + __num(rr) + " 0 0 1 " + __num(-ww) + " 0 Z ";
    });
    return path(d.trim(), __place(props, { name: name, w: w, h: h, fill: accent }));
  }
  if (kind === "zones") {
    var cols = data, zgap = props.gap === undefined ? 2 : props.gap, bar = props.bar || 12;
    var free = w - zgap * (cols.length - 1), zl = [];
    var shares = props.widths || cols.map(function () { return 1; });
    var total = shares.reduce(function (s, v) { return s + v; }, 0) || 1;
    var labelled = props.labels && props.labels.length, st = labelled ? __labelStyle(props) : null;
    var zx = 0, rad = props.radius === undefined ? bar / 2 : props.radius;
    cols.forEach(function (c, i) {
      var first = i === 0, lastz = i === cols.length - 1, zw = free * (shares[i] || 0) / total;
      zl.push(rect({ name: "Zone", x: __num(zx), y: props.value === undefined ? 0 : 10, w: __num(zw), h: bar, fill: c,
        radius: props.each ? rad : [first ? rad : 0, lastz ? rad : 0, lastz ? rad : 0, first ? rad : 0] }));
      if (labelled && props.labels[i] !== undefined) {
        zl.push(text(props.labels[i], st, { color: ink, w: __num(zw), align: "center", x: __num(zx), y: (props.value === undefined ? 0 : 10) + bar + 6 }));
      }
      zx += zw + zgap;
    });
    if (props.value !== undefined) {
      var mx = Math.max(0, Math.min(1, props.value)) * w;
      zl.push(path("M0 0 L12 0 L6 8 Z", { name: "Marker", w: 12, h: 8, x: __num(mx - 6), y: 0, fill: __tok(props.marker, ["text", "#111111"]) }));
    }
    var zh = (props.value === undefined ? 0 : 10) + bar + (labelled ? 26 : 0);
    return frame(__place(props, { name: name, w: w, h: zh }), zl);
  }
  var vals = data.map(Number), lo = Math.min.apply(null, vals), hi = Math.max.apply(null, vals);
  if (props.zero !== false && kind === "bars") lo = Math.min(0, lo);
  var span = hi - lo || 1;
  if (kind === "bars") {
    var gap = props.gap === undefined ? 4 : props.gap, bw = (w - gap * (vals.length - 1)) / vals.length;
    var hl = props.highlight === undefined ? vals.length - 1 : props.highlight;
    var hasLabels = props.labels && props.labels.length, lst = (hasLabels || props.values) ? __labelStyle(props) : null;
    var plot = hasLabels ? h - 22 : h;
    var bars = [];
    vals.forEach(function (v, i) {
      var bh = Math.max(2, (v - lo) / span * plot * (props.values ? 0.86 : 1));
      var on = hl === "all" || i === hl || (Array.isArray(hl) && hl.indexOf(i) >= 0);
      var x = __num(i * (bw + gap)), rad = props.radius === undefined ? Math.min(4, bw / 2) : props.radius;
      if (props.behind) bars.push(rect({ name: "Track", x: x, y: 0, w: __num(bw), h: __num(plot), radius: rad, fill: track }));
      var b = rect({ name: on ? "Bar/now" : "Bar", x: x, y: __num(plot - bh), w: __num(bw), h: __num(bh), radius: rad, fill: on ? accent : (props.behind ? __hexAlpha(accent, 0.45) : track) });
      if (props.glow && on) b.effects = [{ type: "drop-shadow", x: 0, y: 0, blur: 12, color: __hexAlpha(accent, 0.55) }];
      bars.push(b);
      if (props.values && props.values[i] !== undefined) {
        bars.push(text(props.values[i], lst, { color: on ? accent : ink, w: __num(bw), align: "center", x: x, y: __num(Math.max(0, plot - bh - 18)) }));
      }
      if (hasLabels && props.labels[i] !== undefined) {
        bars.push(text(props.labels[i], lst, { color: ink, w: __num(bw), align: "center", x: x, y: __num(plot + 6) }));
      }
    });
    return frame(__place(props, { name: name, w: w, h: h }), bars);
  }
  if (kind !== "spark" && kind !== "area") __fail("chart kind is spark, area, bars, ring, wave or zones");
  var pad = (props.weight || 2) + 2;
  var pts = vals.map(function (v, i) { return [i / Math.max(1, vals.length - 1) * (w - pad * 2) / w + pad / w, (1 - (v - lo) / span) * (h - pad * 2) / h + pad / h]; });
  var line = __smooth(pts, w, h);
  var marks = [];
  if (kind === "area" && props.grid) {
    var gd = "";
    for (var gy = 1; gy < props.grid; gy++) gd += "M0 " + __num(gy * h / props.grid) + " L" + w + " " + __num(gy * h / props.grid) + " ";
    for (var gx = 1; gx < props.grid * 2; gx++) gd += "M" + __num(gx * w / (props.grid * 2)) + " 0 L" + __num(gx * w / (props.grid * 2)) + " " + h + " ";
    marks.push(path(gd.trim(), { name: "Grid", w: w, h: h, stroke: __tok(props.gridColor, ["line", "#E5E7EB"]), weight: 1 }));
  }
  var under = line + " L" + __num(pts[pts.length - 1][0] * w) + " " + h + " L" + __num(pts[0][0] * w) + " " + h + " Z";
  if (kind === "area") {
    marks.push(path(under, { name: "Area", w: w, h: h, fill: { type: "linear", angle: 90, stops: [__hexAlpha(accent, 0.45), __hexAlpha(accent, 0)] } }));
  } else if (props.area !== false) {
    marks.push(path(under, { name: "Area", w: w, h: h, fill: { color: accent, opacity: 0.12 } }));
  }
  var ln = path(line, { name: "Line", w: w, h: h, stroke: accent, weight: props.weight || (kind === "area" ? 3 : 2) });
  if (props.glow) ln.effects = [{ type: "drop-shadow", x: 0, y: 0, blur: 10, color: __hexAlpha(accent, 0.7) }];
  marks.push(ln);
  if (kind === "spark") {
    var last = pts[pts.length - 1];
    marks.push(ellipse({ name: "Now", x: __num(last[0] * w - 3.5), y: __num(last[1] * h - 3.5), w: 7, h: 7, fill: accent }));
  }
  return frame(__place(props, { name: name, w: w, h: h }), marks);
}

// qr("https://...", {size: 160, color: "text", bg: "surface", quiet: 2}): a
// real, scannable QR code as one vector, for a ticket, a pass or a payment.
function qr(data, props) {
  props = props || {};
  var code = __qr(String(data));
  var quiet = props.quiet === undefined ? 2 : props.quiet;
  var size = props.size || 160, mod = size / (code.size + quiet * 2);
  var kids = [path(code.d, { name: "Modules", w: __num(code.size * mod), h: __num(code.size * mod), x: __num(quiet * mod), y: __num(quiet * mod), view: [code.size, code.size], fill: __tok(props.color, ["text", "#000000"]) })];
  return frame(__place(props, { name: props.name || "QR code", w: size, h: size, fill: props.bg === undefined ? null : props.bg, radius: props.radius }), kids);
}

// scrim({w, h, color: "#000000", from: 0, to: 0.8}): a gradient from clear
// to dark (top to bottom), laid over a photo so the text on it reads.
// angle 270 runs it bottom to top.
function scrim(props) {
  props = props || {};
  var c = props.color || "#000000";
  var p = __place(props, { name: props.name || "Scrim", w: props.w || 393, h: props.h || 240 });
  p.fill = { type: "linear", angle: props.angle === undefined ? 90 : props.angle, stops: [__hexAlpha(c, props.from || 0), __hexAlpha(c, props.to === undefined ? 0.8 : props.to)] };
  return rect(p);
}

// notches({w, y, r: 12, color: "bg", dots: true}): the tear line of a
// ticket: a half-circle bite out of each side in the page colour and a row
// of holes between them. Place it inside a ticket card (clip: true) at the
// line's y; it is absolute.
function notches(props) {
  props = props || {};
  var w = props.w || 361, r = props.r || 12, y = props.y || 0;
  var color = __tok(props.color, ["bg", "#F5F5F5"]);
  var kids = [
    ellipse({ name: "Bite left", x: -r, y: 0, w: r * 2, h: r * 2, fill: color }),
    ellipse({ name: "Bite right", x: w - r, y: 0, w: r * 2, h: r * 2, fill: color }),
  ];
  if (props.dots !== false) kids.push(pattern("perforation", { w: __num(w - r * 4), h: r * 2, x: r * 2, y: 0, gap: props.gap || 12, size: props.size || 3, color: __tok(props.dotColor, ["line", "#D4D4D8"]) }));
  return frame({ name: props.name || "Tear line", w: w, h: r * 2, absolute: true, x: 0, y: __num(y - r) }, kids);
}

// trail([[x, y], ...], {w, h, width: 28, color, nodes: [{label, color}]}):
// a winding road through points (shares of the box) with optional round
// stops on it, for a level map, a journey or a learning path.
function trail(points, props) {
  props = props || {};
  var w = props.w || 361, h = props.h || 320, width = props.width || 28;
  var kids = [path(__smooth(points, w, h), { name: "Road", w: w, h: h, stroke: __tok(props.color, ["surface", "#FFF1C2"]), weight: width })];
  (props.nodes || []).forEach(function (nd, i) {
    if (!nd || i >= points.length) return;
    var s = nd.size || width * 1.6;
    var dot = frame({ name: "Stop " + (i + 1), absolute: true, x: __num(points[i][0] * w - s / 2), y: __num(points[i][1] * h - s / 2), w: s, h: s, radius: s / 2,
      fill: nd.color || __tok(undefined, ["brand", "#3B82F6"]), stroke: nd.ring ? { color: nd.ring, width: 4 } : undefined, dir: "row", main: "center", cross: "center" },
      nd.label === undefined ? [] : [text(String(nd.label), nd.style || __labelStyle(props), { color: nd.ink || "on-brand" })]);
    kids.push(dot);
  });
  return frame(__place(props, { name: props.name || "Trail", w: w, h: h }), kids);
}

// pattern("dots" | "lines" | "grid" | "topo" | "perforation", {w, h, color,
// gap, size, seed}): a texture as one vector, for a card's corner, a
// ticket's tear line or the ground under a big number. topo draws contour
// lines; perforation a row of holes across w (give it the page colour).
function pattern(kind, props) {
  props = props || {};
  var w = props.w || 120, h = props.h || 120, gap = props.gap || 12, size = props.size || 1.5;
  var color = __tok(props.color, ["line", "text-muted", "#D4D4D8"]);
  var d = "";
  var base = __place(props, { name: props.name || "Pattern/" + kind, w: w, h: h });
  function dot(x, y, r) { return "M" + __num(x - r) + " " + __num(y) + " a" + r + " " + r + " 0 1 0 " + 2 * r + " 0 a" + r + " " + r + " 0 1 0 " + -2 * r + " 0 "; }
  if (kind === "dots") {
    for (var y = gap / 2; y < h; y += gap) for (var x = gap / 2; x < w; x += gap) d += dot(x, y, size);
    base.fill = color;
    return path(d.trim(), base);
  }
  if (kind === "perforation") {
    var r = props.size || 4;
    for (var px = gap / 2; px < w; px += gap) d += dot(px, h / 2, r);
    base.fill = color;
    return path(d.trim(), base);
  }
  if (kind === "lines") {
    for (var lx = -h; lx < w; lx += gap) d += "M" + lx + " " + h + " L" + (lx + h) + " 0 ";
    var lines = path(d.trim(), { name: "Lines", w: w, h: h, stroke: color, weight: size });
    var lf = frame(base, [lines]);
    lf.clip = true;
    return lf;
  }
  if (kind === "grid") {
    for (var gx = gap; gx < w; gx += gap) d += "M" + gx + " 0 L" + gx + " " + h + " ";
    for (var gy = gap; gy < h; gy += gap) d += "M0 " + gy + " L" + w + " " + gy + " ";
    base.stroke = color;
    base.weight = props.size || 1;
    return path(d.trim(), base);
  }
  if (kind === "topo") {
    var rnd = __rng(props.seed), cx = w * (0.3 + rnd() * 0.4), cy = h * (0.3 + rnd() * 0.4);
    var p1 = rnd() * 6, p2 = rnd() * 6, levels = props.levels || 9, reach = Math.sqrt(w * w + h * h) * 0.75;
    for (var lv = 1; lv <= levels; lv++) {
      var rad = reach * lv / levels, pts = [];
      for (var a = 0; a <= 72; a++) {
        var t = a / 72 * Math.PI * 2;
        var rr = rad * (1 + 0.16 * Math.sin(3 * t + p1 + lv * 0.3) + 0.08 * Math.sin(5 * t + p2));
        pts.push([(cx + rr * Math.cos(t)) / w, (cy + rr * Math.sin(t) * 0.8) / h]);
      }
      d += __poly(pts, w, h, true) + " ";
    }
    var contours = path(d.trim(), { name: "Contours", w: w, h: h, stroke: color, weight: props.size || 1 });
    var tf = frame(base, [contours]);
    tf.clip = true;
    return tf;
  }
  if (kind === "stars") {
    // Eight-pointed stars (two squares turned 45 degrees), the geometric
    // motif of tile, carpet and carved wood across Anatolia and the Levant.
    var sr = props.size && props.size > 3 ? props.size : gap * 0.36;
    for (var sy = gap / 2, rowi = 0; sy < h + sr; sy += gap, rowi++) {
      for (var sx = (rowi % 2 ? gap : gap / 2); sx < w + sr; sx += gap) {
        var sq = [], dm = [];
        for (var c = 0; c < 4; c++) {
          var ta = Math.PI / 4 + c * Math.PI / 2, tb = c * Math.PI / 2;
          sq.push([(sx + sr * Math.cos(ta)) / w, (sy + sr * Math.sin(ta)) / h]);
          dm.push([(sx + sr * 1.41 * 0.72 * Math.cos(tb)) / w, (sy + sr * 1.41 * 0.72 * Math.sin(tb)) / h]);
        }
        d += __poly(sq, w, h, true) + " " + __poly(dm, w, h, true) + " ";
      }
    }
    var stars = path(d.trim(), { name: "Stars", w: w, h: h, stroke: color, weight: props.weight || 1.25 });
    var sf = frame(base, [stars]);
    sf.clip = true;
    return sf;
  }
  __fail("pattern kind is dots, lines, grid, topo, stars or perforation");
}

// sprig({w: 140, h: 160, leaves: 7, fill: "tile-1", seed: "olive"}): a
// branch of leaves as one vector, for the corner of a header or a tile.
// lean bends the stem (-1 to 1); flip mirrors it.
function sprig(props) {
  props = props || {};
  var w = props.w || 120, h = props.h || 160, count = props.leaves || 7;
  var rnd = __rng(props.seed || "sprig");
  var lean = props.lean === undefined ? 0.35 : props.lean;
  var fill = __tok(props.fill, ["brand", "text-muted", "#5B7F4A"]);
  var flip = props.flip ? -1 : 1;
  function X(x) { return flip > 0 ? x : w - x; }
  // The stem runs from the bottom up, bending sideways by lean.
  var stem = [];
  for (var i = 0; i <= 10; i++) {
    var t = i / 10;
    stem.push([w * (0.5 + lean * 0.35 * Math.sin(t * Math.PI * 0.9)), h * (1 - t * 0.96)]);
  }
  var d = "";
  var sw = Math.max(1.2, w / 90);
  for (var s = 0; s < stem.length - 1; s++) {
    var a = stem[s], b = stem[s + 1];
    var nx = -(b[1] - a[1]), ny = b[0] - a[0], nl = Math.sqrt(nx * nx + ny * ny) || 1;
    nx = nx / nl * sw / 2; ny = ny / nl * sw / 2;
    d += "M" + __num(X(a[0] - nx)) + " " + __num(a[1] - ny) + " L" + __num(X(b[0] - nx)) + " " + __num(b[1] - ny) +
      " L" + __num(X(b[0] + nx)) + " " + __num(b[1] + ny) + " L" + __num(X(a[0] + nx)) + " " + __num(a[1] + ny) + " Z ";
  }
  for (var l = 0; l < count; l++) {
    var tt = 0.12 + 0.84 * (l / Math.max(1, count - 1));
    var k = Math.min(9, Math.floor(tt * 10)), f = tt * 10 - k;
    var px = stem[k][0] + (stem[k + 1][0] - stem[k][0]) * f, py = stem[k][1] + (stem[k + 1][1] - stem[k][1]) * f;
    var side = l % 2 ? 1 : -1;
    var len = Math.min(w, h) * (0.32 + rnd() * 0.12) * (1 - tt * 0.35);
    var ang = -Math.PI / 2 + side * (0.75 + rnd() * 0.25);
    var tx = px + Math.cos(ang) * len, ty = py + Math.sin(ang) * len;
    var mx = (px + tx) / 2, my = (py + ty) / 2, ox = -(ty - py) * 0.28, oy = (tx - px) * 0.28;
    d += "M" + __num(X(px)) + " " + __num(py) + " Q" + __num(X(mx + ox)) + " " + __num(my + oy) + " " + __num(X(tx)) + " " + __num(ty) +
      " Q" + __num(X(mx - ox)) + " " + __num(my - oy) + " " + __num(X(px)) + " " + __num(py) + " Z ";
  }
  var base = __place(props, { name: props.name || "Illustration/sprig", w: w, h: h, fill: fill });
  return path(d.trim(), base);
}

// __pt is the point at deg degrees (clockwise from 12 o'clock) on a circle.
function __pt(cx, cy, r, deg) {
  var t = (deg - 90) * Math.PI / 180;
  return [cx + r * Math.cos(t), cy + r * Math.sin(t)];
}

// __arc is an SVG arc from a0 to a1 degrees (clockwise from 12 o'clock).
function __arc(cx, cy, r, a0, a1) {
  // A full turn is two half arcs: one arc's ends would meet and vanish.
  if (a1 - a0 >= 359.99) return __arc(cx, cy, r, a0, a0 + 180) + " " + __arc(cx, cy, r, a0 + 180, a0 + 360 - 0.001).replace(/^M[^A]*/, "");
  var p0 = __pt(cx, cy, r, a0), p1 = __pt(cx, cy, r, a1);
  return "M" + __num(p0[0]) + " " + __num(p0[1]) + " A" + __num(r) + " " + __num(r) + " 0 " + (a1 - a0 > 180 ? 1 : 0) + " 1 " + __num(p1[0]) + " " + __num(p1[1]);
}

// dial({w: 260, from: -150, to: 150, ticks: 50, major: 5, labels: ["0", ...,
// "10"], value: 0.5, knob: "surface", accent: "brand", shadow: "raised"}):
// a rotary control: a scale of ticks with numbers around it, a round knob
// and the pointer at value (0-1 along the scale). For gain, volume,
// temperature or a timer set by hand.
function dial(props) {
  props = props || {};
  var w = props.w || 240, a0 = props.from === undefined ? -150 : props.from, a1 = props.to === undefined ? 150 : props.to;
  var n = props.ticks || 50, major = props.major || 5, labels = props.labels || [];
  var ink = __tok(props.ink, ["text", "#111111"]), tickC = __tok(props.tick, ["text-muted", "text", "#666666"]);
  var accent = __tok(props.accent, ["brand", "#FF5A1F"]);
  var c = w / 2, lab = labels.length ? Math.max(18, w * 0.09) : 0, ro = c - lab, d = "";
  for (var i = 0; i <= n; i++) {
    var a = a0 + (a1 - a0) * i / n, big = i % major === 0;
    var p = __pt(c, c, ro, a), q = __pt(c, c, ro - (big ? w * 0.05 : w * 0.028), a);
    d += "M" + __num(p[0]) + " " + __num(p[1]) + " L" + __num(q[0]) + " " + __num(q[1]) + " ";
  }
  var kids = [path(d.trim(), { name: "Ticks", w: w, h: w, stroke: tickC, weight: Math.max(1, w / 220) })];
  if (labels.length) {
    var st = __labelStyle(props), lh = __system.type[st] ? __system.type[st].line_height : 16;
    labels.forEach(function (l, j) {
      var lp = __pt(c, c, c - lab / 2, a0 + (a1 - a0) * j / Math.max(1, labels.length - 1));
      kids.push(text(String(l), st, { color: ink, w: 32, align: "center", x: __num(lp[0] - 16), y: __num(lp[1] - lh / 2) }));
    });
  }
  var kr = ro - w * 0.09;
  var rim = __tok(props.rim, ["line", "#D0D0CC"]);
  var knob = { name: "Knob", x: __num(c - kr), y: __num(c - kr), w: __num(kr * 2), h: __num(kr * 2), fill: __tok(props.knob, ["surface", "#F2F2F0"]), stroke: { color: rim, width: 2 } };
  if (props.shadow) knob.shadow = props.shadow;
  kids.push(ellipse(knob));
  kids.push(ellipse({ name: "Knob/face", x: __num(c - kr * 0.8), y: __num(c - kr * 0.8), w: __num(kr * 1.6), h: __num(kr * 1.6), fill: null, stroke: { color: rim, width: 1 } }));
  var va = a0 + (a1 - a0) * Math.max(0, Math.min(1, props.value === undefined ? 0.5 : props.value));
  var pa = __pt(c, c, kr * 0.92, va), pb = __pt(c, c, kr * 0.74, va);
  kids.push(path("M" + __num(pa[0]) + " " + __num(pa[1]) + " L" + __num(pb[0]) + " " + __num(pb[1]), { name: "Pointer", w: w, h: w, stroke: accent, weight: Math.max(2.5, w / 80) }));
  return frame(__place(props, { name: props.name || "Dial", w: w, h: w }), kids);
}

// meter({h: 280, values: [0.72, 0.64], scale: ["0", "-10", "-20", "-30",
// "-40", "-50", "-60"], peak: 0.8}): a level meter, one ladder of segments
// per channel lit up to its value, the segments above peak in the accent,
// with the scale beside it. For audio input, signal or load.
function meter(props) {
  props = props || {};
  var vals = props.values || [props.value === undefined ? 0.6 : props.value];
  var scale = props.scale || [], st = scale.length ? __labelStyle(props) : null;
  var sw = scale.length ? (props.scaleWidth || 30) : 0, h = props.h || 240, gap = props.gap || 8, bar = props.bar || 10;
  var w = props.w || (sw + 6 + vals.length * (bar + gap));
  var on = __tok(props.color, ["text", "#F2F2F2"]), off = __tok(props.track, ["line", "#3A3A3A"]), hot = __tok(props.accent, ["brand", "#FF5A1F"]);
  var peak = props.peak === undefined ? 0.85 : props.peak, step = props.step || 4, seg = Math.max(1, step - 1.5);
  var kids = [];
  vals.forEach(function (v, ch) {
    var x = sw + 6 + ch * (bar + gap), lit = "", dim = "", red = "";
    for (var y = h - seg; y >= 0; y -= step) {
      var share = (h - y) / h, d = "M" + __num(x) + " " + __num(y) + " h" + bar + " v" + __num(seg) + " h" + -bar + " Z ";
      if (share > v) dim += d; else if (share > peak) red += d; else lit += d;
    }
    if (dim) kids.push(path(dim.trim(), { name: "Off", w: w, h: h, fill: off }));
    if (lit) kids.push(path(lit.trim(), { name: "Level", w: w, h: h, fill: on }));
    if (red) kids.push(path(red.trim(), { name: "Peak", w: w, h: h, fill: hot }));
  });
  if (scale.length) {
    var lh = __system.type[st] ? __system.type[st].line_height : 14;
    scale.forEach(function (s, i) {
      kids.push(text(String(s), st, { color: __tok(props.ink, ["text-muted", "#999999"]), w: sw, align: "right", x: 0, y: __num(Math.min(h - lh, Math.max(0, i / Math.max(1, scale.length - 1) * h - lh / 2))) }));
    });
  }
  return frame(__place(props, { name: props.name || "Meter", w: w, h: h }), kids);
}

// arcRoute([x0, y0], [x1, y1], {w, h, bend: 0.25, progress: 0.6, labels:
// ["IST", "LHR"], color, labelStyle, glow: true}): a curved flight path
// between two points (shares of the box): the flown part solid and lit,
// the rest dotted, a dot at each end with its code and a plane at
// progress. Lay it over a dark map or a world picture.
function arcRoute(from, to, props) {
  props = props || {};
  var w = props.w || 361, h = props.h || 300, bend = props.bend === undefined ? 0.25 : props.bend;
  var color = __tok(props.color, ["brand", "#9CC8FF"]), halo = __tok(props.halo, ["bg", "#0B1220"]);
  var A = [from[0] * w, from[1] * h], B = [to[0] * w, to[1] * h];
  var dx = B[0] - A[0], dy = B[1] - A[1];
  // The curve bows upward, away from the bottom of the box.
  var C = [(A[0] + B[0]) / 2 + dy * bend, (A[1] + B[1]) / 2 - dx * bend];
  if (C[1] > (A[1] + B[1]) / 2) C = [(A[0] + B[0]) / 2 - dy * bend, (A[1] + B[1]) / 2 + dx * bend];
  function at(t) { var u = 1 - t; return [u * u * A[0] + 2 * u * t * C[0] + t * t * B[0], u * u * A[1] + 2 * u * t * C[1] + t * t * B[1]]; }
  var prog = Math.max(0, Math.min(1, props.progress === undefined ? 0.6 : props.progress)), steps = 48, flown = "", rest = "";
  for (var i = 0; i <= steps; i++) {
    var t = i / steps, p = at(t), xy = __num(p[0]) + " " + __num(p[1]);
    if (t <= prog + 1e-9) flown += (flown ? " L" : "M") + xy;
    if (t >= prog - 1 / steps) rest += (rest ? " L" : "M") + xy;
  }
  var kids = [];
  if (rest && prog < 1) kids.push(path(rest, { name: "Route/ahead", w: w, h: h, stroke: __hexAlpha(color, 0.7), weight: 1.5, dash: [2, 5] }));
  if (flown && prog > 0) {
    var f = path(flown, { name: "Route/flown", w: w, h: h, stroke: color, weight: 2.5 });
    if (props.glow !== false) f.effects = [{ type: "drop-shadow", x: 0, y: 0, blur: 8, color: __hexAlpha(color, 0.8) }];
    kids.push(f);
  }
  var st = props.labels ? __labelStyle(props) : null, lh = st && __system.type[st] ? __system.type[st].line_height : 16;
  [A, B].forEach(function (p, i) {
    kids.push(ellipse({ name: "End", x: __num(p[0] - 5), y: __num(p[1] - 5), w: 10, h: 10, fill: color, stroke: { color: halo, width: 2, align: "outside" } }));
    if (props.labels && props.labels[i]) kids.push(text(props.labels[i], st, { color: __tok(props.ink, ["text", "#FFFFFF"]), w: 48, align: "center", x: __num(p[0] - 24), y: __num(p[1] - 10 - lh) }));
  });
  if (props.plane !== false && prog > 0 && prog < 1) {
    var pp = at(prog), pn = at(Math.min(1, prog + 0.01));
    var deg = Math.atan2(pn[1] - pp[1], pn[0] - pp[0]) * 180 / Math.PI;
    kids.push(icon("plane", { size: 24, color: color, absolute: true, x: __num(pp[0] - 12), y: __num(pp[1] - 12), rotation: __num(deg + 45) }));
  }
  return frame(__place(props, { name: props.name || "Flight route", w: w, h: h }), kids);
}
