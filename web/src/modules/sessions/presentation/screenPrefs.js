// What this browser chose for the session screen: the terminal's text size
// and focus mode. Kept in the shared per-browser preferences, which can be
// missing or throw: the defaults then apply to this page only.

/** The terminal's text sizes, smallest first. */
export const TEXT_SIZES = Object.freeze([
  Object.freeze({ px: 12, label: "Smaller" }),
  Object.freeze({ px: 13, label: "Default" }),
  Object.freeze({ px: 15, label: "Larger" }),
]);
export const DEFAULT_TEXT_SIZE = 13;

const TEXT_KEY = "terminal-text-size";
const FOCUS_KEY = "session-focus";

/**
 * @param {import("../../../shared/infrastructure/storage.js").Preferences} prefs
 */
export function screenPrefs(prefs) {
  return {
    /** @returns {number} one of TEXT_SIZES' px */
    textSize() {
      const px = Number(prefs.get(TEXT_KEY));
      return TEXT_SIZES.some((s) => s.px === px) ? px : DEFAULT_TEXT_SIZE;
    },
    /** @param {number} px */
    setTextSize(px) {
      if (!TEXT_SIZES.some((s) => s.px === px)) return;
      prefs.set(TEXT_KEY, px === DEFAULT_TEXT_SIZE ? null : String(px));
    },
    focus() {
      return prefs.get(FOCUS_KEY) === "on";
    },
    /** @param {boolean} on */
    setFocus(on) {
      prefs.set(FOCUS_KEY, on ? "on" : null);
    },
  };
}

/** The next size up, wrapping to the smallest: the phone sheet's one Text size row. */
export function nextTextSize(px) {
  const at = TEXT_SIZES.findIndex((s) => s.px === px);
  return TEXT_SIZES[(at + 1) % TEXT_SIZES.length].px;
}
