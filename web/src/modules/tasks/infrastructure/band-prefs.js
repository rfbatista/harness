// The context strip's open panel over the task_band cookie: one key, one
// preference per browser for every task. The server reads the same cookie
// (internal/adapter/in/web/sessions/band.go), so the first paint has the
// panel open.

const NAME = "task_band";
const YEAR = 365 * 24 * 60 * 60;
const KEYS = ["reviews", "feature", "token-savers", "none"];

/**
 * @param {{ cookie: string }} [doc]  the document, or a stand-in in tests
 * @returns {import("../domain/ports.js").BandPrefs}
 */
export function cookieBandPrefs(doc = globalThis.document) {
  return {
    open() {
      const pair = doc.cookie.split(";").map((c) => c.trim()).find((c) => c.startsWith(`${NAME}=`));
      const value = pair ? pair.slice(NAME.length + 1) : "";
      return KEYS.includes(value) ? value : "";
    },
    setOpen(key) {
      if (!KEYS.includes(key)) return;
      doc.cookie = `${NAME}=${key}; Path=/; Max-Age=${YEAR}; SameSite=Lax`;
    },
  };
}
