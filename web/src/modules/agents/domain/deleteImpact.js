// What the delete dialog says (UX spec section 6, Deletes): everything the
// item reaches, the name to type, and what Undo can and cannot do. The server
// removes the item and every reference to it; this only reads the catalog
// the page holds, so the dialog opens at once.
//
//   skill   the agents that list it and the bundles that hold it
//   bundle  the agents and bundles that name it
//   server  the agents and projects that get it
//   agent   nothing: the catalog read does not say which projects enable it

/** @typedef {import("./ports.js").Catalog} Catalog */
/** @typedef {"agent"|"skill"|"bundle"|"mcp"} DeleteKind */

const enc = encodeURIComponent;
const plural = (n, word) => `${n} ${word}${n === 1 ? "" : "s"}`;

/** Said in every delete dialog. The catalog is not in git: this is the only safety net, and it says so. */
export const DELETE_UNDO_NOTE =
  "Undo in the message that follows puts everything back, until the next catalog change or a server restart. The catalog is not in git, so there is no other copy.";

/** The files each kind of delete changes, as the catalog read names their versions. */
const FILES = {
  agent: ["agents.json"],
  bundle: ["agents.json"],
  skill: ["agents.json"],
  mcp: ["agents.json", "mcp-servers.json"],
};

/**
 * The `base_versions` a delete sends: the versions the page read of the files it changes.
 * @param {Pick<Catalog, "versions">} catalog
 * @param {DeleteKind} kind
 */
export function deleteBaseVersions(catalog, kind) {
  return Object.fromEntries((FILES[kind] ?? []).filter((f) => f in catalog.versions).map((f) => [f, catalog.versions[f]]));
}

const agentItems = (names) => names.map((n) => ({ name: n, href: `/agents/${enc(n)}` }));
const bundleItems = (names) => names.map((n) => ({ name: `@${n}`, href: `/agents/bundles/${enc(n)}` }));

/**
 * @typedef {object} DeleteDialog
 * @property {DeleteKind} kind
 * @property {string} name
 * @property {string} typed  what the person must type to confirm: the name, without a bundle's @
 * @property {string} title
 * @property {string} button
 * @property {string} path  what the catalog loses
 * @property {string} intro
 * @property {{ key: string, label: string, items: { name: string, href: string }[] }[]} groups  non-empty groups only
 * @property {boolean} reachesNothing
 * @property {string} nothing  said when it reaches nothing
 * @property {string} total  the sentence after the groups, or ""
 * @property {string} note  what Undo can and cannot do
 */

/**
 * The dialog for deleting `name`, or null when the catalog has no such item.
 * @param {Catalog} catalog
 * @param {string} kind
 * @param {string} name
 * @returns {DeleteDialog | null}
 */
export function deleteImpact(catalog, kind, name) {
  const base = { kind, name, typed: name, note: DELETE_UNDO_NOTE, total: "", nothing: "" };
  const done = (d) => ({ ...d, groups: d.groups.filter((g) => g.items.length > 0), reachesNothing: d.groups.every((g) => g.items.length === 0) });
  if (kind === "agent") {
    if (!catalog.agents.some((a) => a.name === name)) return null;
    return done({
      ...base,
      title: `Delete agent “${name}”?`,
      button: "Delete agent",
      path: `agents.json › agents.${name}`,
      intro: "This removes the agent from the catalog. Its skills, bundles and MCP servers stay in the catalog. Sessions already running keep what they started with.",
      groups: [],
    });
  }
  if (kind === "skill") {
    const s = catalog.skills.find((x) => x.name === name);
    if (!s) return null;
    return done({
      ...base,
      title: `Delete skill “${name}”?`,
      button: "Delete skill",
      path: `skills/${name}/`,
      intro: `This removes the folder skills/${name}/ and takes the skill out of every place that names it:`,
      nothing: "No agent or bundle lists it.",
      groups: [
        { key: "agents", label: "Agents that list it directly", items: agentItems(s.agents) },
        { key: "bundles", label: "Bundles that hold it", items: bundleItems(s.bundles) },
      ],
      total: s.agentCount > 0 ? `It reaches ${plural(s.agentCount, "agent")} in all.` : "",
    });
  }
  if (kind === "bundle") {
    const b = catalog.bundles.find((x) => x.name === name);
    if (!b) return null;
    return done({
      ...base,
      title: `Delete bundle “@${name}”?`,
      button: "Delete bundle",
      path: `agents.json › bundles.${name}`,
      intro: `This removes the bundle and takes @${name} out of every place that names it. Its skills stay in the catalog:`,
      nothing: "No agent or bundle names it.",
      groups: [
        { key: "agents", label: "Agents that name it", items: agentItems(b.usedByAgents) },
        { key: "bundles", label: "Bundles that name it", items: bundleItems(b.usedByBundles) },
      ],
      total: b.usedByAgents.length > 0 ? `Those agents lose up to ${plural(b.resolvedSkills.length, "skill")}.` : "",
    });
  }
  if (kind === "mcp") {
    const m = catalog.mcpServers.find((x) => x.name === name);
    if (!m) return null;
    return done({
      ...base,
      title: `Delete MCP server “${name}”?`,
      button: "Delete server",
      path: `mcp-servers.json › mcpServers.${name}`,
      intro: "This removes the server and stops giving it to:",
      nothing: "No agent or project gets it.",
      groups: [
        { key: "agents", label: "Agents that get it", items: agentItems(m.agents) },
        { key: "projects", label: "Projects that get it", items: m.projects.map((p) => ({ name: p.name, href: `/projects/${enc(p.id)}/settings` })) },
      ],
    });
  }
  return null;
}
