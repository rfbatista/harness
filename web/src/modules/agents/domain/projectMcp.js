// What a project's settings show of the catalog's MCP servers (UX spec
// section 7): a checklist row per server, the selection compared with what is
// saved, the servers a catalog change took off the project, and the What a
// session gets example. A variable's value is never here, nor a plain env or
// header value: only the ${VAR} references and whether each is set.

import { varNames } from "./mcp.js";

/** @typedef {import("./ports.js").Catalog} Catalog */

const plural = (n, word) => `${n} ${word}${n === 1 ? "" : "s"}`;

function whoElse(server, projectId) {
  const others = server.projects.filter((p) => p.id !== projectId).length;
  if (server.agents.length === 0 && others === 0) return "Unused";
  const parts = [];
  if (server.agents.length > 0) parts.push(plural(server.agents.length, "agent"));
  if (others > 0) parts.push(`${plural(others, "other project")}`);
  return `Used by ${parts.join(" and ")}`;
}

/**
 * @param {Catalog} catalog
 * @param {string} projectId
 * @param {string[]} selected
 */
export function projectMcpRows(catalog, projectId, selected) {
  return catalog.mcpServers.map((s) => {
    const known = new Map(s.envRefs.map((r) => [r.name, r.set]));
    const names = [];
    for (const text of [s.command, ...s.args, s.url, ...Object.values(s.env), ...Object.values(s.headers)]) {
      for (const n of varNames(String(text))) if (!names.includes(n)) names.push(n);
    }
    const vars = names.map((n) => {
      const set = known.get(n) ?? false;
      return { label: "${" + n + "}", state: set ? "set" : "not set", tone: set ? "success" : "attention" };
    });
    const unset = vars.some((v) => v.tone === "attention");
    return {
      name: s.name,
      transport: s.transport,
      checked: selected.includes(s.name),
      vars,
      who: whoElse(s, projectId),
      warning: unset ? "the server fails to start in a session until the variable is set" : "",
      warningText: unset ? " · the server fails to start in a session until the variable is set" : "",
    };
  });
}

/** Whether two selections differ, as sets. @param {string[]} saved @param {string[]} selected */
export function selectionChanged(saved, selected) {
  const a = new Set(saved);
  const b = new Set(selected);
  return a.size !== b.size || [...a].some((n) => !b.has(n));
}

/** The names the catalog has, in its order. @param {Catalog} catalog @param {string[]} names */
export function projectMcpSaved(catalog, names) {
  return catalog.mcpServers.map((s) => s.name).filter((n) => names.includes(n));
}

/**
 * The servers the project had that the catalog no longer holds. When the new
 * list holds a name the old one lacked, a server was renamed (the server
 * rewrites the list): which name went cannot be told, so none is named.
 * @param {string[]} before  the project's saved list before the read
 * @param {Catalog} catalog  the catalog read now
 * @param {string[]} [after]  the project's saved list read now
 */
export function removedFromProject(before, catalog, after = before) {
  if (after.some((n) => !before.includes(n))) return [];
  const have = new Set(catalog.mcpServers.map((s) => s.name));
  return before.filter((n) => !have.has(n));
}

/**
 * The What a session gets example: an agent that has servers (else the first
 * agent) with the project's, and a session with no agent.
 * @param {Catalog} catalog
 * @param {string[]} selected
 */
export function sessionExample(catalog, selected) {
  const a = catalog.agents.find((x) => x.mcpServers.length > 0) ?? catalog.agents[0];
  const mine = projectMcpSaved(catalog, selected);
  const own = a ? a.mcpServers : [];
  const withAgent = [
    ...own.map((name) => ({ name, origin: mine.includes(name) ? "both, counted once" : "agent" })),
    ...mine.filter((n) => !own.includes(n)).map((name) => ({ name, origin: "project" })),
  ];
  return { agent: a?.name ?? "", withAgent, withoutAgent: mine };
}
