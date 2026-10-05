// A browser key press as the terminal socket wants it: ports.KeyEvent, in
// ultraviolet's key codes (terminal-keys.json, pinned to the server's
// library by a Go test). The server encodes it for the program's modes.

import keys from "./terminal-keys.json" with { type: "json" };

/**
 * @typedef {{ code: number, text?: string, mod?: number }} KeyEvent
 */

/**
 * The KeyEvent for a keydown, or null for keys the browser should keep:
 * lone modifiers, dead keys, and Cmd/⊞ shortcuts (copy, paste, tabs).
 * @param {{ key: string, shiftKey: boolean, altKey: boolean, ctrlKey: boolean, metaKey: boolean }} e
 * @returns {KeyEvent | null}
 */
export function toKeyEvent(e) {
  if (e.metaKey) return null;
  const mod = (e.shiftKey ? keys.mods.shift : 0) | (e.altKey ? keys.mods.alt : 0) | (e.ctrlKey ? keys.mods.ctrl : 0);

  const special = keys.codes[e.key];
  if (special !== undefined) return withMod({ code: special }, mod);

  const chars = [...e.key];
  if (chars.length !== 1) return null; // Shift, Control, Dead, Unidentified, …

  if (e.ctrlKey || e.altKey) {
    // ctrl+c, alt+b: the key's base character; the server encodes the chord.
    return withMod({ code: e.key.toLowerCase().codePointAt(0) }, mod);
  }
  // Printable: sent as text, which already carries shift (A, !, é).
  return { code: e.key.codePointAt(0), text: e.key };
}

function withMod(event, mod) {
  return mod ? { ...event, mod } : event;
}
