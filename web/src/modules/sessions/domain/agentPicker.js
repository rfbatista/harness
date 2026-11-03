// What the agent pickers (the new-session form and the brainstorm dialog)
// say about the project's enabled agents (ADR 0038, contract section 8). Pure.

import { Codes, codeOf } from "../../../shared/domain/errors.js";

/** The project settings' Enabled agents section. @param {string} projectId */
export const agentsSettingsHref = (projectId) => `/projects/${encodeURIComponent(projectId)}/settings#settings-agents-enabled`;

/** A repository's Agents tab, where an agent is assigned again. @param {string} projectId @param {string} repositoryId */
export const repositoryAgentsHref = (projectId, repositoryId) =>
  `/projects/${encodeURIComponent(projectId)}/repositories/${encodeURIComponent(repositoryId)}/architecture/agents`;

/** How many of the picker's missing agents are unassigned from the repository (the others are not enabled in the project). @param {{ reason: string }[]} notOffered */
export const unassignedCount = (notOffered) => notOffered.filter((n) => n.reason === "unassigned").length;

/**
 * The line over the picker: how many it offers; nothing when none. Past the
 * agents unassigned from the repository it says how many are enabled.
 * @param {number} count @param {number} [unassigned]
 */
export const pickerCount = (count, unassigned = 0) => (count > 0 ? (unassigned > 0 ? `${count} of ${count + unassigned} enabled` : `${count} enabled`) : "");

/**
 * The hint under the picker. With agents unassigned from the repository it
 * counts the assigned ones.
 * @param {{ count: number, total: number, mode: "all" | "selected", project: string, repository?: string, unassigned?: number }} p
 */
export function pickerHint({ count, total, mode, project, repository = "", unassigned = 0 }) {
  if (repository && unassigned > 0) {
    const noun = count === 1 ? "agent" : "agents";
    return `Showing the ${count} ${noun} assigned to ${repository}, of ${count + unassigned} enabled in ${project}. ${unassigned} ${unassigned === 1 ? "is" : "are"} unassigned here.`;
  }
  if (mode === "all") return `All agents are enabled in ${project}: every agent in the catalog is offered, and new agents appear automatically.`;
  return `Showing the ${count} ${count === 1 ? "agent" : "agents"} enabled in ${project} of ${total} in the catalog.`;
}

/**
 * Whether a message of the page feed means the picker's list may have moved:
 * a catalog event that names this project, names the catalog itself (an agent
 * was added, renamed or deleted), or names nothing. Another project's list
 * does not.
 * @param {unknown} data @param {string} projectId
 */
export function catalogChangeTouches(data, projectId) {
  // An agent was assigned or unassigned, or a repository prompt changed: this project's pickers re-read.
  if (data !== null && typeof data === "object" && !Array.isArray(data) && "repository_agents_changed" in data) {
    const body = /** @type {any} */ (data).repository_agents_changed;
    return !body || typeof body.project_id !== "string" || body.project_id === projectId;
  }
  if (data === null || typeof data !== "object" || Array.isArray(data) || !("agent_catalog_changed" in data)) return false;
  const body = /** @type {any} */ (data).agent_catalog_changed;
  const paths = (body && Array.isArray(body.paths) ? body.paths : []).filter((p) => typeof p === "string");
  if (paths.length === 0) return true;
  return paths.some((p) => !p.startsWith("projects/") || p === `projects/${projectId}`);
}

/**
 * The banner for a start the server refused because the project does not
 * enable the agent; null for any other error.
 * @param {unknown} err
 * @param {{ agent: string, project: string, projectId: string }} ctx
 */
export function notEnabledRefusal(err, { agent, project, projectId }) {
  if (codeOf(err) !== Codes.AGENT_NOT_ENABLED) return null;
  return {
    message: `The session was not started: ${agent} is not enabled in ${project}.`,
    code: Codes.AGENT_NOT_ENABLED,
    explanation: `${project} enables only the agents on its list, and ${agent} is not on it. Pick one of the agents offered, or enable ${agent} in the project's settings. Your prompt is kept.`,
    linkText: `Open ${project}'s Agents settings`,
    href: agentsSettingsHref(projectId),
  };
}

/**
 * The missing agents behind the disclosure, each with why and where to fix it.
 * @param {{ name: string, reason: "unassigned" | "not_enabled" }[]} notOffered
 * @param {{ repository: string, repositoryId: string, project: string, projectId: string }} ctx
 */
export function notOfferedRows(notOffered, { repository, repositoryId, project, projectId }) {
  return notOffered.map((n) =>
    n.reason === "not_enabled"
      ? { name: n.name, reason: `not enabled in ${project}`, linkText: `Enable in ${project}`, href: agentsSettingsHref(projectId) }
      : { name: n.name, reason: `unassigned from ${repository}`, linkText: `Manage agents in ${repository}`, href: repositoryAgentsHref(projectId, repositoryId) },
  );
}

/**
 * The banner for a start the server refused because the agent is not
 * assigned to the repository; null for any other error. The server's own
 * details name the repository, falling back to what the form knows.
 * @param {unknown} err
 * @param {{ agent: string, repository: string, repositoryId: string, projectId: string }} ctx
 */
export function notAssignedRefusal(err, { agent, repository, repositoryId, projectId }) {
  if (codeOf(err) !== Codes.AGENT_NOT_ASSIGNED) return null;
  const details = /** @type {any} */ (err).details ?? {};
  const repo = (typeof details.repository_name === "string" && details.repository_name) || repository;
  const id = (typeof details.repository_id === "string" && details.repository_id) || repositoryId;
  return {
    message: `The session was not started: ${agent} is not assigned to ${repo}.`,
    code: Codes.AGENT_NOT_ASSIGNED,
    explanation: `${repo} no longer lets ${agent} run there. Pick one of the agents offered, or assign ${agent} again in the repository's settings. Your prompt is kept.`,
    linkText: `Open ${repo}'s Agents settings`,
    href: repositoryAgentsHref(projectId, id),
  };
}
