// Per-browser preferences (theme, collapsed panes). Storage can be missing or
// throw (private windows, blocked site data), so every access is guarded and
// the UI must work without it.

/**
 * @typedef {object} Preferences
 * @property {(key: string) => string|null} get
 * @property {(key: string, value: string|null) => void} set  null removes the key
 */

const PREFIX = "harness:";

/** @returns {Preferences} */
export function preferences(storage = safeLocalStorage()) {
  return {
    get(key) {
      try {
        return storage?.getItem(PREFIX + key) ?? null;
      } catch {
        return null;
      }
    },
    set(key, value) {
      try {
        if (value === null) storage?.removeItem(PREFIX + key);
        else storage?.setItem(PREFIX + key, value);
      } catch {
        // Not persisted; the choice still applies to this page.
      }
    },
  };
}

function safeLocalStorage() {
  try {
    return globalThis.localStorage ?? null;
  } catch {
    return null;
  }
}
