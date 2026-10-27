// What the agent pickers (the new-session form and the brainstorm dialog)
// say about the project's enabled agents (ADR 0038, contract section 8). Pure.

import { Codes, codeOf } from "../../../shared/domain/errors.js";

/** The project settings' Enabled agents section. @param {string} projectId */
export const agentsSettingsHref = (projectId) => `/projects/${encodeURIComponent(projectId)}/settings#settings-agents-enabled`;

/** The line over the picker: how many it offers; nothing when none. @param {number} count */
export const pickerCount = (count) => (count > 0 ? `${count} enabled` : "");

/**
 * The hint under the picker.
 * @param {{ count: number, total: number, mode: "all" | "selected", project: string }} p
 */
export function pickerHint({ count, total, mode, project }) {
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
