"use strict";

// Key chords as the renderer names them ("Ctrl+Shift+G"), worked out from a
// key press the main process sees before a page does. The browser view uses
// this to pass on only the shortcuts of the app, and to leave AltGr alone.

const CODE_KEYS = { Slash: "/", Comma: ",", Period: ".", Backquote: "`" };

/** The key a physical key stands for, whatever the layout typed with it. */
function keyOfCode(code) {
  const letter = /^Key([A-Z])$/.exec(code ?? "");
  if (letter) return letter[1];
  const digit = /^Digit(\d)$/.exec(code ?? "");
  if (digit) return digit[1];
  return CODE_KEYS[code] ?? "";
}

/**
 * The chord of a key press, or "" when it is not one. On Windows AltGr is
 * Ctrl+Alt: when that pair typed a character other than the key's own (@, €,
 * {, [, ], \, |, ~ and the like), the press is text and never a shortcut.
 * @param {{key: string, code: string, control?: boolean, meta?: boolean, alt?: boolean, shift?: boolean}} input
 */
function chordOf(input) {
  const ctrl = !!(input.control || input.meta);
  if (!ctrl && !input.alt && !/^F\d{1,2}$/.test(input.key)) return "";
  const own = keyOfCode(input.code);
  const typed = input.key ?? "";
  if (ctrl && input.alt && typed.length === 1 && own && typed !== own && typed !== own.toLowerCase()) return "";
  if (ctrl && input.alt && typed.length === 1 && !own && /[^\w]/.test(typed)) return "";
  const parts = [];
  if (ctrl) parts.push("Ctrl");
  if (input.alt) parts.push("Alt");
  if (input.shift) parts.push("Shift");
  parts.push(own || (typed.length === 1 ? typed.toUpperCase() : typed));
  return parts.join("+");
}

module.exports = { chordOf, keyOfCode };
