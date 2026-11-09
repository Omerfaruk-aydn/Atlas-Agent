// Streamed text arrives in bursts: nothing for a moment, then a paragraph. The
// reader sees it at an even pace instead. A cursor chases the end of the text;
// the further behind it is, the faster it moves, and it always stops at the end
// of a word, never inside a link target or a piece of inline code.

import { useEffect, useRef, useState } from "react";
import { reducedMotion } from "./motion";

/** Characters to reveal in a step of about 30 ms, given how far behind the cursor is. */
export function stepSize(pending: number): number {
  if (pending <= 0) return 0;
  // Close behind: a few characters, a calm typing pace. Far behind: catch up
  // in about a second however much arrived.
  return Math.max(2, Math.ceil(pending / 28));
}

/** Moves a cut point forward to the end of the word, unless that would split a token that must stay whole. */
export function wordEnd(text: string, from: number, limit: number): number {
  let at = Math.min(Math.max(from, 0), text.length);
  while (at < text.length && at < limit + 24 && !/\s/.test(text[at] ?? "")) at++;
  if (at >= text.length) return text.length;
  const head = text.slice(0, at);
  // Never stop between "](" and ")" of a link, or inside an open code span.
  const link = head.lastIndexOf("](");
  if (link >= 0 && head.indexOf(")", link) < 0) {
    const close = text.indexOf(")", at);
    if (close >= 0 && close - at < 400) return close + 1;
  }
  const ticks = (head.match(/`/g) ?? []).length;
  if (ticks % 2 === 1) {
    const close = text.indexOf("`", at);
    if (close >= 0 && close - at < 120) return close + 1;
  }
  return at;
}

/** The next cut: `shown` characters are on screen, the text is `text`. */
export function nextCut(text: string, shown: number): number {
  const pending = text.length - shown;
  if (pending <= 0) return text.length;
  const target = shown + stepSize(pending);
  return wordEnd(text, target, target);
}

const FRAME_MS = 30;

/**
 * The text to draw. While `live`, it trails the real text and catches up; when
 * the stream ends (or motion is reduced, or the window is hidden) it is the
 * whole text at once.
 */
export function useSmoothedText(text: string, live: boolean): string {
  const [shown, setShown] = useState(live ? 0 : text.length);
  const target = useRef(text);
  const cursor = useRef(live ? 0 : text.length);
  target.current = text;

  useEffect(() => {
    if (!live || reducedMotion() || document.hidden) {
      cursor.current = text.length;
      setShown(text.length);
      return;
    }
    let raf = 0;
    let last = 0;
    const tick = (now: number) => {
      raf = requestAnimationFrame(tick);
      if (now - last < FRAME_MS) return;
      last = now;
      const full = target.current;
      if (cursor.current >= full.length) return;
      cursor.current = nextCut(full, cursor.current);
      setShown(cursor.current);
    };
    raf = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(raf);
    // The loop reads the latest text through the ref; only a change of mode restarts it.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [live]);

  // Text that shrank (an edit, a new turn in the same slot) must not leave the cursor past its end.
  if (shown > text.length) return text;
  return live ? text.slice(0, shown) : text;
}
