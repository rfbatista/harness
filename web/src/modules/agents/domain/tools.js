// What the Tools tab shows: Claude Code's built-in tools, read-only, each
// with the agents whose allowed tools name it.

/** @typedef {import("./ports.js").Catalog} Catalog */

export const TOOL_FILTERS = [
  { id: "all", label: "All" },
  { id: "unused", label: "Unused" },
];

const enc = encodeURIComponent;
const allowedBy = (catalog, tool) => catalog.agents.filter((a) => a.allowedTools.includes(tool)).map((a) => a.name);

/** @param {Catalog} catalog @param {{ query?: string, filter?: string }} [o] */
export function toolRows(catalog, { query = "", filter = "all" } = {}) {
  const q = query.trim().toLowerCase();
  return catalog.builtInTools
    .map((t) => ({ t, agents: allowedBy(catalog, t.name) }))
    .filter(({ t, agents }) => (filter !== "unused" || agents.length === 0) && (!q || t.name.toLowerCase().includes(q) || t.description.toLowerCase().includes(q)))
    .map(({ t, agents }) => ({
      name: t.name,
      href: `/agents/tools#${enc(t.name)}`,
      description: t.description,
      meta: agents.length === 0 ? "no agent" : `${agents.length} agent${agents.length === 1 ? "" : "s"}`,
    }));
}

export function toolsFooter() {
  return "Claude Code built-in tools · read-only";
}

/** @param {Catalog} catalog @param {string} name */
export function toolDetail(catalog, name) {
  const t = catalog.builtInTools.find((x) => x.name === name);
  if (!t) return null;
  const agents = allowedBy(catalog, name);
  return {
    name: t.name,
    description: t.description,
    unused: agents.length === 0,
    allowedBy: agents.map((n) => ({ name: n, href: `/agents/${enc(n)}` })),
    heading: `Allowed by · ${agents.length}`,
  };
}
