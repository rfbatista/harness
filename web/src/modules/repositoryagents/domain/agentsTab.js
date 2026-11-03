// What a repository's Agents tab says about its agents (ADR 0061, UX study
// "Manage agents by repository"). Pure: the Alpine component only renders it.
// Words are the Glossary's: Assigned, Unassign, Assign again, Repository
// prompt, Enabled in.

const plural = (n, one, many = `${one}s`) => `${n} ${n === 1 ? one : many}`;
const names = (list) => list.map((n) => `${n}`).join(", ");

/** The project's Agents settings, where an agent is enabled. @param {string} projectId */
export const enableAgentsHref = (projectId) => `/projects/${encodeURIComponent(projectId)}/settings#settings-agents-enabled`;

/** @param {import("./ports.js").RepositoryAgents} tab */
export function groupAgents(tab) {
  const agents = tab.agents;
  return {
    assigned: agents.filter((a) => a.state === "assigned"),
    notAssigned: agents.filter((a) => a.state === "unassigned"),
    notEnabled: agents.filter((a) => a.state === "not_enabled"),
  };
}

/** The two stats over the list: "4 of 5 assigned here" and the prompts count. @param {import("./ports.js").RepositoryAgents} tab */
export function stats(tab) {
  const { assigned, notAssigned } = groupAgents(tab);
  const withPrompt = tab.agents.filter((a) => a.prompt.set).length;
  return {
    assigned: { value: `${assigned.length} of ${assigned.length + notAssigned.length}`, label: "assigned here" },
    prompts: { value: String(withPrompt), label: `with a repository prompt` },
  };
}

/**
 * The line under the title and the banner the project's mode asks for.
 * @param {import("./ports.js").RepositoryAgents} tab
 */
export function modeNotice(tab) {
  const { project } = tab;
  if (project.agentsMode === "all") return { line: `All agents are enabled in ${project.name}. Unassigning only affects ${tab.repository.name}.`, banner: null };
  if (project.enabledCount === 0) {
    return {
      line: `Agents that can run in ${tab.repository.name}: the agents ${project.name} enables, minus the ones you unassign here.`,
      banner: { tone: "info", text: `${project.name} enables no agent (Selected agents, empty list), so none can be assigned here.`, linkText: `Enable agents in ${project.name}`, href: enableAgentsHref(project.id) },
    };
  }
  return {
    line: `Agents that can run in ${tab.repository.name}: the agents ${project.name} enables, minus the ones you unassign here. Unassigning only affects this repository.`,
    banner: {
      tone: "info",
      text: `${project.name} is in Selected agents mode: ${project.enabledCount} of ${project.catalogCount} catalog agents are enabled. Only those can be assigned here.`,
      linkText: `Change in ${project.name} Agents settings`,
      href: enableAgentsHref(project.id),
    },
  };
}

/** The attention banner when nothing is assigned though the project enables agents; null otherwise. @param {import("./ports.js").RepositoryAgents} tab */
export function nothingAssigned(tab) {
  const { assigned, notAssigned } = groupAgents(tab);
  if (assigned.length > 0 || notAssigned.length === 0) return null;
  return {
    text: `No agent can run in ${tab.repository.name}. You unassigned all ${notAssigned.length} enabled ${notAssigned.length === 1 ? "agent" : "agents"}. A session started here as an agent will be refused; a plain session without an agent still works.`,
  };
}

/**
 * One row of the tab, as it is drawn.
 * @param {import("./ports.js").AgentRow} a
 * @param {import("./ports.js").RepositoryAgents} tab
 */
export function rowModel(a, tab) {
  const project = tab.project.name;
  const repo = tab.repository.name;
  const unassignedHere = a.unassigned;
  const badges = [];
  if (a.state === "assigned") badges.push({ word: "Assigned", tone: "success" });
  if (a.state === "unassigned") badges.push({ word: "Unassigned", tone: "attention" });
  if (a.state === "not_enabled") {
    badges.push({ word: `Not enabled in ${project}`, tone: "danger" });
    if (unassignedHere) badges.push({ word: "Unassigned here", tone: "attention" });
  }
  if (a.prompt.set) badges.push({ word: a.state === "assigned" ? "Repository prompt set" : "Repository prompt kept", tone: "" });
  const notes = [];
  if (a.state === "unassigned") notes.push(`Not offered here. A start in ${repo} is refused.`);
  if (a.state === "not_enabled") {
    notes.push(unassignedHere ? `If ${project} enables it again, it stays unassigned here until you assign it.` : `Enable it in ${project} first. Nothing to assign here.`);
  }
  if (a.previousName) notes.push(`was “${a.previousName}”`);
  return {
    name: a.name,
    description: a.description,
    state: a.state,
    badges,
    summary: a.prompt.set ? `“${a.prompt.summary}”` : "No repository prompt",
    notes,
    promptAction: a.state === "assigned" ? (a.prompt.set ? "Edit prompt" : "Add prompt") : a.prompt.set ? "View prompt" : "Add prompt",
    canUnassign: a.state === "assigned",
    canAssign: a.state === "unassigned" || (a.state === "not_enabled" && unassignedHere),
    // Assign again is offered on an unassigned row; on a not enabled one it is disabled, with its reason in the notes.
    assignDisabled: a.state === "not_enabled",
    assignDisabledReason: a.state === "not_enabled" ? `Enable ${a.name} in ${project} first` : "",
    showAssign: a.state === "unassigned",
    showAssignDisabled: a.state === "not_enabled",
    unassignLabel: `Unassign ${a.name} from ${repo}`,
    assignLabel: `Assign ${a.name} again in ${repo}`,
    whyId: `why-${a.name}`,
    enableHref: a.state === "not_enabled" ? enableAgentsHref(tab.project.id) : "",
    enableText: a.state === "not_enabled" ? `Enable in ${project}` : "",
  };
}

/** @param {{ agent: string, repository: string, running: number }} p */
export function unassignedToast({ agent, repository, running }) {
  const sessions = running > 0 ? ` ${plural(running, "running session")} ${running === 1 ? "continues" : "continue"}.` : "";
  return `Unassigned ${agent} from ${repository}.${sessions} It stays available in other repositories.`;
}

/** @param {{ agent: string, repository: string, promptSet: boolean }} p */
export function assignedToast({ agent, repository, promptSet }) {
  return `${agent} is assigned to ${repository} again.${promptSet ? " Its repository prompt is back in use." : ""}`;
}

/**
 * The banner of a refused stale click: what changed since the page loaded.
 * @param {import("./ports.js").RepositoryAgents} before
 * @param {import("./ports.js").RepositoryAgents} current
 */
export function staleBanner(before, current) {
  const was = new Map(before.agents.map((a) => [a.name, a.state]));
  const changes = [];
  for (const a of current.agents) {
    const old = was.get(a.name);
    if (old === undefined) changes.push(`${a.name} appeared`);
    else if (old !== a.state) changes.push(`${a.name} is now ${a.state === "assigned" ? "assigned" : a.state === "unassigned" ? "unassigned" : "not enabled"}`);
  }
  for (const [name] of was) if (!current.agents.some((a) => a.name === name)) changes.push(`${name} left the list`);
  const what = changes.length > 0 ? ` (${names(changes)})` : "";
  return `The agents list changed while this page was open${what}. Your last action was not applied; the list is up to date, so you can repeat it.`;
}
