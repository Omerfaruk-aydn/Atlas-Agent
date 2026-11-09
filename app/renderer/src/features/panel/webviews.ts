// The pages of the panel browser. Each web tab's page is a <webview> tag, which
// is part of the app's own DOM composition: the app's menus, popovers and
// dialogs draw over it at full quality, with nothing captured and nothing
// parked. The main process owns the page (main/browser.cjs); this module owns
// the tag.
//
// A <webview> that is moved in the DOM is reloaded, so the tags do not live
// inside the panel (which comes and goes, and holds one tab at a time). They
// live in one layer at the root of the app, one per web tab, for as long as
// the tab is open: a page survives the panel closing, another tab being
// shown, and the chat changing. The layer follows the box of the active tab's
// page area (`registerRegion`) every frame, so the page moves with the panel
// while it opens, closes and expands, and is cut where the panel cuts it.
// A tag with no box in front of it is only hidden.

import { usePanel } from "../../stores/panel";

interface Guest {
  el: (HTMLElement & { getWebContentsId(): number }) | null;
  /** The address the page opens with, passed to the main process once the guest is attached. */
  url: string;
  /** The style last written, so a frame with nothing new writes nothing. */
  written: string;
}

const regions = new Map<string, HTMLElement>();
const guests = new Map<string, Guest>();
/** Tabs that have been in front at least once: a restored tab loads its page only when it is first shown. */
const shown = new Set<string>();
let layer: HTMLElement | null = null;
let frame = 0;
/** Counts what the layer has to react to (a tab shown for the first time); read through `useShownVersion`. */
let version = 0;
const listeners = new Set<() => void>();
const bump = () => {
  version++;
  for (const notify of listeners) notify();
};

export const subscribeShown = (notify: () => void) => {
  listeners.add(notify);
  return () => listeners.delete(notify);
};
export const shownVersion = () => version;

/** The element every tag lives in; made by the WebLayer component. */
export function setLayer(el: HTMLElement | null) {
  layer = el;
  if (el) {
    for (const guest of guests.values()) if (guest.el && !guest.el.isConnected) el.appendChild(guest.el);
    start();
  }
}

/** Says where a tab's page goes: the box in the panel. Returns a function that takes it back. */
export function registerRegion(id: string, el: HTMLElement): () => void {
  regions.set(id, el);
  if (!shown.has(id)) {
    shown.add(id);
    bump();
  }
  start();
  return () => {
    if (regions.get(id) === el) regions.delete(id);
  };
}

export function hasBeenShown(id: string): boolean {
  return shown.has(id);
}

export function hasGuest(id: string): boolean {
  return guests.has(id);
}

/** Makes the tag of a tab if it has none: the main process is told first, and only then is the tag put in the page. */
export async function ensureGuest(id: string, url: string): Promise<void> {
  const browser = window.atlas?.browser;
  if (!browser || guests.has(id) || !layer) return;
  const guest: Guest = { el: null, url, written: "" };
  guests.set(id, guest);
  const announced = await browser.expect(id).catch(() => false);
  if (!announced || guests.get(id) !== guest || !layer) {
    if (guests.get(id) === guest) guests.delete(id);
    return;
  }
  const el = document.createElement("webview") as Guest["el"] & HTMLElement;
  // The tag needs an address to make its guest; about:blank has nothing in it. The page itself is loaded by the main process once the guest is attached and guarded.
  el.setAttribute("src", "about:blank");
  el.setAttribute("data-tab", id);
  // The guest has an id once its first (blank) page is ready.
  el.addEventListener(
    "dom-ready",
    () => {
      void browser.attach(id, el.getWebContentsId(), guest.url).then((state) => {
        if (state) usePanel.getState().report(id, state);
      });
    },
    { once: true },
  );
  guest.el = el;
  el.style.visibility = "hidden";
  layer.appendChild(el);
  start();
}

/** The main process gave the keyboard back to the app after loading a page: the tag lets go of it too. */
export function blurGuest(id: string): void {
  const el = guests.get(id)?.el;
  if (el && document.activeElement === el) el.blur();
}

/** Removes the tag of a closed tab; its guest goes with it. */
export function dropGuest(id: string): void {
  const guest = guests.get(id);
  guests.delete(id);
  shown.delete(id);
  regions.delete(id);
  guest?.el?.remove();
}

/** Every tab id that has a tag, or is about to. */
export function guestIds(): string[] {
  return [...guests.keys()];
}

interface Box {
  left: number;
  top: number;
  right: number;
  bottom: number;
}

const intersect = (a: Box, b: Box): Box => ({ left: Math.max(a.left, b.left), top: Math.max(a.top, b.top), right: Math.min(a.right, b.right), bottom: Math.min(a.bottom, b.bottom) });

/** The px values of an `inset(...)` clip-path, top right bottom left. */
function insetOf(value: string): [number, number, number, number] | null {
  const m = /^inset\(([^)]*)\)/.exec(value);
  if (!m) return null;
  const parts = m[1]!.split(/\s+/).map((v) => parseFloat(v));
  if (parts.length === 0 || parts.some((v) => !Number.isFinite(v))) return null;
  const [t, r = t, b = t, l = r] = parts as [number, number?, number?, number?];
  return [t, r!, b!, l!];
}

/**
 * Where the region is on screen and how much of it can be seen: the box, cut
 * by every ancestor that clips its children (the panel slot while it grows,
 * the card, the window) and by the slot's animated clip-path.
 */
export function visibleBox(el: HTMLElement): { box: Box; clip: Box } {
  const r = el.getBoundingClientRect();
  const box: Box = { left: r.left, top: r.top, right: r.right, bottom: r.bottom };
  let clip: Box = { left: 0, top: 0, right: window.innerWidth, bottom: window.innerHeight };
  for (let p = el.parentElement; p && p !== document.body; p = p.parentElement) {
    const cs = getComputedStyle(p);
    const pr = p.getBoundingClientRect();
    if (cs.overflowX !== "visible" || cs.overflowY !== "visible") clip = intersect(clip, { left: pr.left, top: pr.top, right: pr.right, bottom: pr.bottom });
    const inset = cs.clipPath !== "none" ? insetOf(cs.clipPath) : null;
    if (inset) clip = intersect(clip, { left: pr.left + inset[3], top: pr.top + inset[0], right: pr.right - inset[1], bottom: pr.bottom - inset[2] });
  }
  return { box, clip: intersect(clip, box) };
}

function place(guest: Guest, id: string) {
  const el = guest.el;
  if (!el) return;
  const region = regions.get(id);
  let style = "visibility:hidden";
  if (region?.isConnected) {
    const { box, clip } = visibleBox(region);
    const w = box.right - box.left;
    const h = box.bottom - box.top;
    if (w >= 2 && h >= 2 && clip.right - clip.left >= 1 && clip.bottom - clip.top >= 1) {
      const cut = `inset(${clip.top - box.top}px ${box.right - clip.right}px ${box.bottom - clip.bottom}px ${clip.left - box.left}px)`;
      style = `left:${box.left}px;top:${box.top}px;width:${w}px;height:${h}px;clip-path:${cut};visibility:visible`;
    } else {
      // Hidden, but at its last size: the page does not re-flow while it is out of sight.
      style = "visibility:hidden";
    }
  }
  if (style === guest.written) return;
  guest.written = style;
  if (style === "visibility:hidden") el.style.visibility = "hidden";
  else el.style.cssText = style;
}

function tick() {
  frame = 0;
  for (const [id, guest] of guests) place(guest, id);
  if (guests.size > 0) frame = requestAnimationFrame(tick);
}

function start() {
  if (!frame && guests.size > 0) frame = requestAnimationFrame(tick);
  watch();
}

let watching = false;
/**
 * The frame loop follows anything that moves smoothly. A change that happens
 * at once (the panel expanding, the sidebar folding, a bar opening) lands
 * between two frames, so it is also followed the moment the DOM changes,
 * before the next paint: the page is never a frame behind its box.
 */
function watch() {
  if (watching || typeof MutationObserver === "undefined") return;
  watching = true;
  new MutationObserver(() => {
    for (const [id, guest] of guests) place(guest, id);
  }).observe(document.body, { subtree: true, childList: true, attributes: true, attributeFilter: ["data-panel", "data-expanded", "data-sidebar", "data-active", "class", "inert"] });
}
