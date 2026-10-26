// What the MCP servers tab shows (UX spec section 3): rows with filters and
// badges, and a server's detail. A variable's value is never here: only the
// ${VAR} references and whether each is set. A plain-text env or header value
// is hidden too, since it may be a secret the person typed.

import { problemBadge } from "./library.js";

/** @typedef {import("./ports.js").Catalog} Catalog */

export const MCP_FILTERS = [
  { id: "all", label: "All" },
  { id: "stdio", label: "stdio" },
  { id: "http", label: "http" },
  { id: "secret", label: "Needs a secret" },
  { id: "unused", label: "Unused" },
];

const plural = (n, word) => `${n} ${word}${n === 1 ? "" : "s"}`;
const enc = encodeURIComponent;
const unusedServer = (s) => s.agents.length === 0 && s.projects.length === 0;

/**
 * The variables a text names with ${NAME} or ${NAME:-default}, in order, once each.
 * @param {string} text
 * @returns {string[]}
 */
export function varNames(text) {
  const names = [];
  for (const m of text.matchAll(/\$\{([A-Za-z_][A-Za-z0-9_]*)(?::-[^}]*)?\}/g)) if (!names.includes(m[1])) names.push(m[1]);
  return names;
}

/** @param {Catalog} catalog @param {{ query?: string, filter?: string }} [o] */
export function mcpRows(catalog, { query = "", filter = "all" } = {}) {
  const q = query.trim().toLowerCase();
  const keep = {
    all: () => true,
    stdio: (s) => s.transport === "stdio",
    http: (s) => s.transport === "http",
    secret: (s) => s.envRefs.length > 0,
    unused: unusedServer,
  }[filter] ?? (() => true);
  const target = (s) => (s.transport === "stdio" ? [s.command, ...s.args].join(" ") : s.url);
  return catalog.mcpServers.filter((s) => keep(s) && (!q || s.name.toLowerCase().includes(q) || target(s).toLowerCase().includes(q))).map((s) => ({
    name: s.name,
    href: `/agents/mcp/${enc(s.name)}`,
    description: target(s),
    meta: unusedServer(s)
      ? `${s.transport} · Unused`
      : [s.transport, plural(s.agents.length, "agent"), ...(s.projects.length ? [plural(s.projects.length, "project")] : [])].join(" · "),
    badge: problemBadge(s.problems),
  }));
}

/** @param {Pick<Catalog, "mcpServers">} catalog */
export function mcpFooter(catalog) {
  return `mcp-servers.json · ${plural(catalog.mcpServers.length, "server")}`;
}

/**
 * The detail of the server `name`, or null. `checked` is the last Check again
 * answer, which wins over what the catalog read said.
 * @param {Catalog} catalog
 * @param {string} name
 * @param {Record<string, boolean>} checked
 */
export function mcpDetail(catalog, name, checked) {
  const s = catalog.mcpServers.find((x) => x.name === name);
  if (!s) return null;
  const known = new Map(s.envRefs.map((r) => [r.name, r.set]));
  const ref = (n) => {
    const set = checked[n] ?? known.get(n) ?? false;
    return { name: n, label: "${" + n + "}", set, state: set ? "set" : "not set", tone: set ? "success" : "attention" };
  };
  const stdio = s.transport === "stdio";
  const pairs = Object.entries(stdio ? s.env : s.headers);
  const entries = pairs.map(([key, value]) => {
    const refs = varNames(String(value)).map(ref);
    const plain = refs.length === 0;
    return { key, text: plain ? "plain text, hidden" : String(value), plain, refs };
  });
  const everyName = [];
  for (const text of [s.command, ...s.args, s.url, ...Object.values(s.env), ...Object.values(s.headers)]) {
    for (const n of varNames(String(text))) if (!everyName.includes(n)) everyName.push(n);
  }
  const refs = everyName.map(ref);
  const unset = refs.filter((r) => !r.set).map((r) => r.name);
  return {
    name: s.name,
    transport: s.transport,
    unused: unusedServer(s),
    path: `mcp-servers.json › mcpServers.${s.name}`,
    copyPath: `${catalog.root}/mcp-servers.json`,
    targetLabel: stdio ? "Command" : "URL",
    target: stdio ? [s.command, ...s.args].join(" ") : s.url,
    entriesLabel: stdio ? "Env" : "Headers",
    entries,
    refs,
    refNames: everyName,
    unset,
    unsetText: unset.map((n) => "${" + n + "}").join(", "),
    unsetVerb: unset.length === 1 ? "is not set" : "are not set",
    agents: s.agents.map((n) => ({ name: n, href: `/agents/${enc(n)}` })),
    projects: s.projects.map((p) => ({ name: p.name, href: `/projects/${enc(p.id)}/settings` })),
    editHref: `/agents/mcp/${enc(s.name)}/edit`,
    duplicateHref: `/agents/mcp/new?from=${enc(s.name)}`,
  };
}
