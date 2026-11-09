// Motion helpers. CSS does most of the work (see design/motion.css); these
// cover what CSS cannot: keeping a thing mounted while it leaves, and moving
// the page from one state to the next with the old and the new morphing.

import { useEffect, useState } from "react";
import { flushSync } from "react-dom";

/** True when the person (or the system) asked for less motion. */
export function reducedMotion(): boolean {
  return document.documentElement.dataset.motion === "reduce" || matchMedia("(prefers-reduced-motion: reduce)").matches && document.documentElement.dataset.motion !== "full";
}

/** Milliseconds of a CSS duration token, for timers that must match the animation. */
export function tokenMs(name: string, fallback: number): number {
  const raw = getComputedStyle(document.documentElement).getPropertyValue(name).trim();
  const value = parseFloat(raw);
  return Number.isFinite(value) ? (raw.endsWith("ms") ? value : value * 1000) : fallback;
}

/**
 * Marks the page while its window is hidden or minimized, so CSS can pause
 * every animation instead of spending battery on something nobody sees.
 */
export function installVisibilityGate(): void {
  if (typeof document === "undefined") return;
  const root = document.documentElement;
  const sync = () => {
    if (document.hidden) root.dataset.hidden = "";
    else delete root.dataset.hidden;
  };
  document.addEventListener("visibilitychange", sync);
  sync();
}

let running: ViewTransition | null = null;

/**
 * Applies a state change as a view transition: elements with a
 * `view-transition-name` slide to their new place, new ones fade in and removed
 * ones fade out, all with the shared tokens. A change made while another is
 * running skips the old one to its end, so rapid changes never pile up. With
 * reduced motion, or without the API, the change is simply made.
 */
// While a view transition runs the page below is not hit-tested (a press lands
// on the root), so a press ends the transition: the page is live again at once.
if (typeof document !== "undefined" && typeof document.addEventListener === "function") {
  document.addEventListener(
    "pointerdown",
    (e) => {
      if (running && e.target === document.documentElement) running.skipTransition();
    },
    true,
  );
}

export function morph(update: () => void): void {
  if (reducedMotion() || typeof document.startViewTransition !== "function") {
    update();
    return;
  }
  running?.skipTransition();
  // The names are only on while a transition runs (see motion.css), so they
  // cost nothing, and change no stacking, the rest of the time.
  const root = document.documentElement;
  root.dataset.vt = "";
  const transition = document.startViewTransition(() => flushSync(update));
  running = transition;
  // A transition that was skipped rejects its promises; that is the normal end of one.
  transition.ready.catch(() => {});
  transition.updateCallbackDone.catch(() => {});
  transition.finished
    .catch(() => {})
    .finally(() => {
      if (running === transition) {
        running = null;
        delete root.dataset.vt;
      }
    });
}

/**
 * Presence for something that must play an exit: `mounted` stays true until
 * `ms` after `want` went false, and `on` is the state to draw (true one frame
 * after mounting, so the entry transition has something to start from).
 * Flipping `want` again mid-way reverses from where it is.
 */
export function usePresence(want: boolean, ms: number): { mounted: boolean; on: boolean } {
  const [state, setState] = useState({ mounted: want, on: want });
  useEffect(() => {
    if (want) {
      setState((s) => (s.mounted ? s : { ...s, mounted: true }));
      let second = 0;
      const first = requestAnimationFrame(() => {
        second = requestAnimationFrame(() => setState({ mounted: true, on: true }));
      });
      return () => {
        cancelAnimationFrame(first);
        cancelAnimationFrame(second);
      };
    }
    setState((s) => ({ ...s, on: false }));
    const timer = setTimeout(() => setState({ mounted: false, on: false }), ms);
    return () => clearTimeout(timer);
  }, [want, ms]);
  return state;
}
