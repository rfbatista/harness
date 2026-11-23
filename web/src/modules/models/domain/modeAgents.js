// Mode agents (ADR 0103) as the Models page edits them: one draft per session
// mode, a state (All agents or Selected agents), the ticked agents and the
// "No agent" entry. Pure: reading the setting and saving it is the gateway's.

export const ALL = "all";
export const SELECTED = "selected";

/**
 * @typedef {{ mode: string, name: string, state: "all" | "selected", agents: string[], noAgent: boolean }} ModeAgents
 * @typedef {ModeAgents & { saved: ModeAgents, saving: boolean, notice: string }} ModeDraft
 */

/** @param {ModeAgents} entry @returns {ModeDraft} the row as the server holds it */
export function draftOf(entry) {
  const saved = { ...entry, agents: [...entry.agents] };
  return { ...saved, saved, saving: false, notice: "" };
}

/** @param {ModeAgents} a @param {ModeAgents} b */
const same = (a, b) => {
  if (a.state !== b.state) return false;
  if (a.state === ALL) return true;
  return a.noAgent === b.noAgent && a.agents.length === b.agents.length && a.agents.every((n) => b.agents.includes(n));
};

/** @param {ModeDraft} draft the person changed the row since it was read or saved */
export const isDirty = (draft) => !same(draft, draft.saved);

/** @param {ModeDraft} draft a Selected row that allows nothing: no agent and no "No agent" */
export const allowsNothing = (draft) => draft.state === SELECTED && draft.agents.length === 0 && !draft.noAgent;

/**
 * A row that was All agents starts its selection empty, so the person ticks
 * what the mode may start; one that was Selected keeps its ticks while the
 * person looks at All agents.
 * @param {ModeDraft} draft @param {"all" | "selected"} state
 */
export function withState(draft, state) {
  const fresh = state === SELECTED && draft.state === ALL && draft.saved.state === ALL;
  return { ...draft, state, agents: fresh ? [] : draft.agents, noAgent: fresh ? false : draft.noAgent, notice: "" };
}

/** @param {ModeDraft} draft @param {string} agent ticks it, or unticks it when ticked */
export function withAgentToggled(draft, agent) {
  const agents = draft.agents.includes(agent) ? draft.agents.filter((n) => n !== agent) : [...draft.agents, agent];
  return { ...draft, agents, notice: "" };
}

/** @param {ModeDraft} draft */
export function withNoAgentToggled(draft) {
  return { ...draft, noAgent: !draft.noAgent, notice: "" };
}

/** @param {ModeDraft} draft the body of set_mode_agents */
export function toWrite(draft) {
  return { mode: draft.mode, state: draft.state, agents: draft.state === SELECTED ? [...draft.agents] : [], noAgent: draft.state === SELECTED ? draft.noAgent : true };
}

/**
 * The rows after a re-read: a row the person has not changed takes what the
 * server holds (and keeps its notice while the server holds what it saved); a
 * changed one keeps the person's choice, less the agents the catalog no longer
 * has.
 * @param {ModeDraft[]} rows @param {ModeAgents[]} entries @param {string[]} catalogNames
 * @returns {ModeDraft[]}
 */
export function merged(rows, entries, catalogNames) {
  return entries.map((entry) => {
    const fresh = draftOf(entry);
    const old = rows.find((r) => r.mode === entry.mode);
    if (!old) return fresh;
    if (!isDirty(old)) return same(old.saved, entry) ? old : fresh;
    return { ...fresh, state: old.state, agents: old.agents.filter((n) => catalogNames.includes(n)), noAgent: old.noAgent };
  });
}
