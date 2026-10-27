// What the Agents tab shows, computed from the catalog read (UX spec section
// 3): the rows with their filters and problem badges, the footer, and an
// agent's detail. The server resolves skills; this only arranges what it sent
// (bundles as groups, the resolved total, the servers' secrets) for the page.

/**
 * @typedef {import("./ports.js").Catalog} Catalog
 * @typedef {{ label: string, tone: "attention" | "danger" }} Badge
 */

import { enabledInView } from "./enabledIn.js";

export const AGENT_FILTERS = [
  { id: "all", label: "All" },
  { id: "mcp", label: "Has MCP" },
  { id: "problems", label: "Has problems" },
];

const plural = (n, word) => `${n} ${word}${n === 1 ? "" : "s"}`;
const enc = encodeURIComponent;

/**
 * The one badge a list of problems shows: no description first, then missing
 * skills, then whatever else the server reports (its own message).
 * @param {import("./ports.js").Problem[]} problems
 * @returns {Badge | null}
 */
export function problemBadge(problems) {
  if (problems.length === 0) return null;
  if (problems.some((p) => p.code === "NO_DESCRIPTION")) return { label: "No description", tone: "attention" };
  if (problems.some((p) => p.code === "SECRET_NOT_SET")) return { label: "secret not set", tone: "attention" };
  const missing = problems.filter((p) => p.code === "MISSING_SKILL").length;
  if (missing > 0) return { label: `${plural(missing, "missing skill")}`, tone: "danger" };
  return { label: problems[0].message || problems[0].code, tone: "danger" };
}

/**
 * @param {Catalog} catalog
 * @param {{ query?: string, filter?: string }} [o]
 */
export function agentRows(catalog, { query = "", filter = "all" } = {}) {
  const q = query.trim().toLowerCase();
  const keep = {
    all: () => true,
    mcp: (a) => a.mcpServers.length > 0,
    problems: (a) => a.problems.length > 0,
  }[filter] ?? (() => true);
  const matches = (a) => !q || [a.name, ...a.aliases, ...a.entries, ...a.resolvedSkills].some((s) => s.toLowerCase().includes(q));
  return catalog.agents.filter((a) => keep(a) && matches(a)).map((a) => {
    const meta = [plural(a.resolvedSkills.length, "skill")];
    if (a.mcpServers.length) meta.push(`${a.mcpServers.length} MCP`);
    return {
      name: a.name,
      href: `/agents/${enc(a.name)}`,
      description: a.description || "No description",
      dim: a.description === "",
      meta: meta.join(" · "),
      badge: problemBadge(a.problems),
    };
  });
}

/** @param {Pick<Catalog, "agents" | "bundles">} catalog */
export function agentsFooter(catalog) {
  return `agents.json · ${plural(catalog.agents.length, "agent")}, ${plural(catalog.bundles.length, "bundle")}`;
}

/**
 * The detail of the agent named `key`, or null when there is none.
 * @param {Catalog} catalog
 * @param {string} key
 * @param {{ id: string, name: string }[]} [projects]  every project, for Enabled in
 */
export function agentDetail(catalog, key, projects = []) {
  const a = catalog.agents.find((x) => x.name === key);
  if (!a) return null;
  const bundles = new Map(catalog.bundles.map((b) => [b.name, b]));
  const skills = new Set(catalog.skills.map((s) => s.name));
  const servers = new Map(catalog.mcpServers.map((s) => [s.name, s]));
  const missingSkills = new Set(a.missingSkills);

  const entryOf = (e) => {
    if (e.startsWith("@")) {
      const nested = bundles.get(e.slice(1));
      return { label: nested ? `${e} (${nested.resolvedSkills.length})` : e, href: `/agents/bundles/${enc(e.slice(1))}`, missing: !nested };
    }
    return { label: e, href: `/agents/skills/${enc(e)}`, missing: !skills.has(e) };
  };
  const groups = a.entries.filter((e) => e.startsWith("@")).map((e) => {
    const name = e.slice(1);
    const b = bundles.get(name);
    const size = b ? b.resolvedSkills.length : 0;
    return { name: e, href: `/agents/bundles/${enc(name)}`, missing: !b, size, sizeLabel: plural(size, "skill"), entries: b ? b.entries.map(entryOf) : [] };
  });
  const singles = a.entries.filter((e) => !e.startsWith("@")).map((e) => ({ name: e, href: `/agents/skills/${enc(e)}`, missing: missingSkills.has(e) }));

  const edit = `/agents/${enc(a.name)}/edit`;
  const dropped = a.droppedDuplicates;
  return {
    key: a.name,
    description: a.description,
    noDescription: a.description === "",
    aliases: a.aliases,
    badge: problemBadge(a.problems),
    problems: a.problems,
    path: `agents.json › agents.${a.name}`,
    copyPath: `${catalog.root}/agents.json`,
    skillsHeading: `Skills · ${a.resolvedSkills.length} after bundles`,
    groups,
    singles,
    droppedNote: dropped.length ? `${plural(dropped.length, "duplicate")} dropped: ${dropped.join(", ")}` : "",
    mcpHeading: `MCP servers · ${a.mcpServers.length}`,
    noServers: a.mcpServers.length === 0,
    servers: a.mcpServers.map((n) => {
      const s = servers.get(n);
      return {
        name: n,
        href: `/agents/mcp/${enc(n)}`,
        found: s !== undefined,
        transport: s?.transport ?? "",
        refs: (s?.envRefs ?? []).map((r) => ({ label: "${" + r.name + "}", set: r.set, state: r.set ? "set" : "not set", tone: r.set ? "success" : "attention" })),
      };
    }),
    tools: a.allowedTools,
    toolsDefault: a.allowedTools.length === 0,
    enabled: enabledInView(catalog, a, projects),
    editHref: edit,
    duplicateHref: `/agents/new?from=${enc(a.name)}`,
    change: { identity: `${edit}#identity`, skills: `${edit}#skills`, mcp: `${edit}#mcp`, tools: `${edit}#tools`, projects: `${edit}#projects` },
  };
}
