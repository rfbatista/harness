// The Agents page's five tabs and its routes (UX spec section 2). The server
// paints the same routes (internal/adapter/in/web/agents/route.go);
// web/testdata/views/agents-routes.json pins them together.

/**
 * @typedef {"agents"|"skills"|"bundles"|"mcp"|"tools"} TabId
 * @typedef {object} Tab
 * @property {TabId} id
 * @property {string} label
 * @property {string} href  the tab's page
 * @property {string} newLabel  the header's New button; "" for none
 * @property {string} newHref
 */

/** @type {Tab[]} */
export const TABS = [
  { id: "agents", label: "Agents", href: "/agents", newLabel: "New agent", newHref: "/agents/new" },
  { id: "skills", label: "Skills", href: "/agents/skills", newLabel: "New skill", newHref: "/agents/skills/new" },
  { id: "bundles", label: "Bundles", href: "/agents/bundles", newLabel: "New bundle", newHref: "/agents/bundles/new" },
  { id: "mcp", label: "MCP servers", href: "/agents/mcp", newLabel: "New MCP server", newHref: "/agents/mcp/new" },
  { id: "tools", label: "Tools", href: "/agents/tools", newLabel: "", newHref: "" },
];

const TAB_SEGMENT = { skills: "skills", bundles: "bundles", mcp: "mcp", tools: "tools" };

/**
 * What a path shows: the tab, the item opened on it, and "new" or "edit" for
 * a full-page editor. null when the path is not the Agents page's.
 * @param {string} pathname
 * @returns {{ tab: TabId, item: string, mode: "" | "new" | "edit" } | null}
 */
export function routeOf(pathname) {
  const parts = pathname.split("/").filter(Boolean);
  if (parts[0] !== "agents") return null;
  let rest = parts.slice(1);
  /** @type {TabId} */
  let tab = "agents";
  if (rest[0] && TAB_SEGMENT[rest[0]]) {
    tab = /** @type {TabId} */ (TAB_SEGMENT[rest[0]]);
    rest = rest.slice(1);
  }
  let decoded;
  try {
    decoded = rest.map(decodeURIComponent);
  } catch {
    return null;
  }
  if (tab === "tools") return decoded.length === 0 ? { tab, item: "", mode: "" } : null;
  if (decoded.length === 0) return { tab, item: "", mode: "" };
  if (decoded.length === 1) return decoded[0] === "new" ? { tab, item: "", mode: "new" } : { tab, item: decoded[0], mode: "" };
  if (decoded.length === 2 && decoded[1] === "edit") return { tab, item: decoded[0], mode: "edit" };
  return null;
}

/**
 * The tab a key moves to from `current`, or null when the key is not a tab key.
 * @param {TabId} current
 * @param {string} key
 * @returns {TabId | null}
 */
export function tabAfterKey(current, key) {
  const at = TABS.findIndex((t) => t.id === current);
  switch (key) {
    case "ArrowRight":
      return TABS[(at + 1) % TABS.length].id;
    case "ArrowLeft":
      return TABS[(at - 1 + TABS.length) % TABS.length].id;
    case "Home":
      return TABS[0].id;
    case "End":
      return TABS[TABS.length - 1].id;
    default:
      return null;
  }
}
