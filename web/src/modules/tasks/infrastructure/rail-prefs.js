// RailPrefs over the rail_collapsed cookie: comma-separated status keys, one
// preference per browser for every project. The shell reads the same cookie
// (internal/adapter/in/web/shell/collapse.go), so the first paint is folded.

const NAME = "rail_collapsed";
const YEAR = 365 * 24 * 60 * 60;
const KEY = /^[a-z_]+$/;

/**
 * @param {{ cookie: string }} [doc]  the document, or a stand-in in tests
 * @returns {import("../domain/ports.js").RailPrefs}
 */
export function cookieRailPrefs(doc = globalThis.document) {
  return {
    collapsed() {
      const pair = doc.cookie.split(";").map((c) => c.trim()).find((c) => c.startsWith(`${NAME}=`));
      if (!pair) return [];
      const keys = pair.slice(NAME.length + 1).split(",").map((k) => k.trim()).filter(Boolean);
      return keys.every((k) => KEY.test(k)) ? [...new Set(keys)] : [];
    },
    setCollapsed(keys) {
      const value = [...new Set(keys)].filter((k) => KEY.test(k)).join(",");
      doc.cookie = `${NAME}=${value}; Path=/; Max-Age=${value ? YEAR : 0}; SameSite=Lax`;
    },
  };
}
