// What a project's settings show of its enabled agents (UX spec section 9,
// contract section 8): the mode (all, or only a selected list), the checklist
// of every catalog agent, the dirty rule, the empty-list warning, and the
// agents a catalog change took off the project. The list the person built is
// kept while All agents is on but ignored: it is never saved or counted then.

/** @typedef {import("./ports.js").Catalog} Catalog */
/** @typedef {{ mode: "all" | "selected", agents: string[] }} ProjectAgents */

export const ALL = "all";
export const SELECTED = "selected";

/** Past this many agents the checklist becomes a search combobox with chips. */
export const COMBOBOX_PAST = 15;
const MAX_OPTIONS = 50;

const plural = (n, word) => `${n} ${word}${n === 1 ? "" : "s"}`;

/** One checklist row per catalog agent. @param {Catalog} catalog @param {string[]} ticked */
export function agentRows(catalog, ticked) {
  return catalog.agents.map((a) => ({
    name: a.name,
    description: a.description.trim() === "" ? "No description" : a.description,
    described: a.description.trim() !== "",
    checked: ticked.includes(a.name),
  }));
}

/** What the project has saved, as the catalog knows it: names in catalog order. @param {Catalog} catalog @param {ProjectAgents} answer @returns {ProjectAgents} */
export function agentsSaved(catalog, answer) {
  if (answer.mode !== SELECTED) return { mode: ALL, agents: [] };
  return { mode: SELECTED, agents: catalog.agents.map((a) => a.name).filter((n) => answer.agents.includes(n)) };
}

/**
 * The list the person ticks from. A list kept from before (even an empty one)
 * stays; with none, switching to Selected ticks every current agent so
 * nothing changes until something is unticked.
 * @param {Catalog} catalog @param {string[] | null} kept
 */
export function startSelected(catalog, kept) {
  return kept ?? catalog.agents.map((a) => a.name);
}

/**
 * Whether the page differs from what is saved. The list counts only while
 * Selected is on.
 * @param {ProjectAgents} saved @param {string} mode @param {string[] | null} ticked
 */
export function agentsChanged(saved, mode, ticked) {
  if (saved.mode !== mode) return true;
  if (mode !== SELECTED) return false;
  const a = new Set(saved.agents);
  const b = new Set(ticked ?? []);
  return a.size !== b.size || [...a].some((n) => !b.has(n));
}

/** The body of set_project_agents: All sends no list. @param {Catalog} catalog @param {string} mode @param {string[] | null} ticked @returns {ProjectAgents} */
export function agentsToWrite(catalog, mode, ticked) {
  if (mode !== SELECTED) return { mode: ALL, agents: [] };
  return agentsSaved(catalog, { mode: SELECTED, agents: ticked ?? [] });
}

/** The attention banner: Selected with nothing ticked. @param {string} mode @param {string[] | null} ticked */
export function emptySelection(mode, ticked) {
  return mode === SELECTED && (ticked ?? []).length === 0;
}

/** @param {string} mode @param {string[] | null} ticked @param {number} total */
export function agentsCount(mode, ticked, total) {
  if (total === 0) return "";
  return mode === SELECTED ? `${(ticked ?? []).length} of ${total} enabled` : `All ${total} enabled`;
}

/** @param {string} mode */
export function agentsHint(mode) {
  return mode === SELECTED
    ? "Only the agents ticked below are enabled. An agent created later is not enabled until you add it."
    : "Every agent in the catalog is enabled, including agents created later.";
}

/**
 * The agents the project had that the catalog no longer holds. When the new
 * list holds a name the old one lacked, an agent was renamed (the server
 * rewrites the list): which name went cannot be told, so none is named.
 * @param {ProjectAgents} before @param {Catalog} catalog @param {ProjectAgents} [after]
 */
export function removedAgents(before, catalog, after = before) {
  if (before.mode !== SELECTED) return [];
  if (after.agents.some((n) => !before.agents.includes(n))) return [];
  const have = new Set(catalog.agents.map((a) => a.name));
  return before.agents.filter((n) => !have.has(n));
}

/** The combobox: unticked agents matching the query, and a chip per ticked one. @param {Catalog} catalog @param {string[]} ticked @param {string} query */
export function agentPicker(catalog, ticked, query) {
  const q = query.trim().toLowerCase();
  const matching = catalog.agents.filter((a) => !ticked.includes(a.name) && (q === "" || a.name.toLowerCase().includes(q) || a.description.toLowerCase().includes(q)));
  return {
    options: matching.slice(0, MAX_OPTIONS).map((a) => ({ key: a.name, label: a.name, hint: a.description.trim() === "" ? "No description" : a.description })),
    hidden: Math.max(0, matching.length - MAX_OPTIONS),
    empty: matching.length === 0,
    chips: catalog.agents.filter((a) => ticked.includes(a.name)).map((a) => a.name),
  };
}

/** How a toast names what the project's agents are now. @param {ProjectAgents} s */
export function agentsPhrase(s) {
  return s.mode === SELECTED ? `enables ${plural(s.agents.length, "agent")}` : "enables all agents";
}
