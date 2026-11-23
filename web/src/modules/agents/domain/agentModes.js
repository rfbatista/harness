// Where an agent may start by session mode (ADR 0103): the detail's Available
// in modes line, and the editor's Modes section, where the Selected modes that
// list the agent are ticked and the All agents modes show as included and
// fixed (they are changed on the Models page). Pure.

/** The session modes in the order a person reads them, with the names the Models page uses. */
export const MODES = [
  { value: "", name: "Default" },
  { value: "architect", name: "Architect" },
  { value: "implement", name: "Implement" },
  { value: "design", name: "Design" },
  { value: "product", name: "Product" },
  { value: "designsystem", name: "Design system" },
  { value: "review", name: "Reviewer" },
  { value: "test", name: "Tester" },
  { value: "qa", name: "QA" },
  { value: "operate", name: "Operator" },
];

/** @typedef {Record<string, "all" | "selected">} ModeStates  each mode's state, as the server said it; a mode it did not name is All agents */
/** @typedef {{ name: string, availableInModes?: string[] }} ModedAgent */

const modeId = (value) => value || "default";
const plural = (n, one, many = `${one}s`) => `${n} ${n === 1 ? one : many}`;

/** @param {ModedAgent} agent the Available in modes section of an agent's detail */
export function availableModesView(agent) {
  const on = new Set(agent.availableInModes ?? []);
  const chips = MODES.filter((m) => on.has(m.value)).map((m) => ({ value: m.value, name: m.name }));
  return {
    heading: "Available in modes",
    chips,
    nowhere: chips.length === 0,
    nowhereText: `No session can start as ${agent.name}. Tick it in a Selected-agents mode on its Modes section, or switch a mode to All agents on the Models page.`,
  };
}

/** The Selected modes that list the agent now, by the catalog. @param {{ agents: ModedAgent[] }} catalog @param {string} name  "" for a new agent @param {ModeStates} states */
export function listedModes(catalog, name, states) {
  const a = name ? catalog.agents.find((x) => x.name === name) : undefined;
  return a ? (a.availableInModes ?? []).filter((v) => states[v] === "selected") : [];
}

/** The modes ticked in the section: what the person set, or else what the catalog says. */
const ticked = (catalog, draft, states) => draft.modes ?? listedModes(catalog, draft.previousName, states);

/**
 * The Modes section of the editor: a row per mode, the Selected ones ticked when
 * they list the agent, the All agents ones fixed and ticked.
 * @param {{ agents: ModedAgent[] }} catalog
 * @param {{ previousName: string, modes: string[] | null }} draft
 * @param {ModeStates} states
 */
export function modeChoices(catalog, draft, states) {
  const on = new Set(ticked(catalog, draft, states));
  const rows = MODES.map((m) => {
    const fixed = states[m.value] !== "selected";
    return {
      value: m.value,
      name: m.name,
      fixed,
      checked: fixed || on.has(m.value),
      badge: fixed ? "All agents" : "Selected agents",
      label: `${m.name}, ${fixed ? "All agents" : "Selected agents"}`,
      tone: fixed ? "" : "signal",
      inputId: `agent-mode-${modeId(m.value)}`,
    };
  });
  const all = rows.filter((r) => r.fixed).length;
  const sel = rows.filter((r) => !r.fixed && r.checked).length;
  return {
    rows,
    summary: `Available in ${rows.length === 0 ? 0 : all + sel} of ${rows.length} modes · ${all} by All agents, ${plural(sel, "Selected-agents list", "Selected-agents lists")}`,
    tocCount: String(all + sel),
  };
}

/** The modes ticked after the person flips `value`. @param {{ agents: ModedAgent[] }} catalog @param {{ previousName: string, modes: string[] | null }} draft @param {ModeStates} states @param {string} value */
export function toggleMode(catalog, draft, states, value) {
  const now = ticked(catalog, draft, states);
  return now.includes(value) ? now.filter((x) => x !== value) : [...now, value];
}

const sameSet = (a, b) => a.length === b.length && a.every((x) => b.includes(x));

/** Whether the section differs from the catalog: an untouched one never does. @param {{ agents: ModedAgent[] }} catalog @param {{ previousName: string, modes: string[] | null }} draft @param {ModeStates} states */
export function modesChanged(catalog, draft, states) {
  return draft.modes !== null && !sameSet(draft.modes, listedModes(catalog, draft.previousName, states));
}

/**
 * `modes` of the save: the full set of Selected modes that list the agent, or
 * undefined (leave the lists as they are) when the section was untouched or put
 * back as it was.
 * @param {{ agents: ModedAgent[] }} catalog
 * @param {{ previousName: string, modes: string[] | null }} draft
 * @param {ModeStates} states
 * @returns {string[] | undefined}
 */
export function modesToSend(catalog, draft, states) {
  return modesChanged(catalog, draft, states) ? [.../** @type {string[]} */ (draft.modes)] : undefined;
}

/**
 * Review change: the mode lists the change sets, as a diff of each list.
 * @param {{ mode: string, name: string, before: string[], after: string[] }[]} entries
 */
export function modeChangeFiles(entries) {
  return entries
    .filter((m) => m.before.join("\n") !== m.after.join("\n"))
    .map((m) => {
      const lines = [
        ...m.before.filter((n) => !m.after.includes(n)).map((n) => ({ kind: "del", mark: "−", text: n })),
        ...m.after.filter((n) => !m.before.includes(n)).map((n) => ({ kind: "add", mark: "+", text: n })),
        ...m.after.filter((n) => m.before.includes(n)).map((n) => ({ kind: "same", mark: " ", text: n })),
      ].map((l, key) => ({ ...l, key }));
      const added = lines.filter((l) => l.kind === "add").length;
      const removed = lines.filter((l) => l.kind === "del").length;
      return { path: `${m.name} · mode agents`, id: `modes/${modeId(m.mode)}`, label: `+${added} −${removed}`, lines, empty: m.after.length === 0, emptyNote: m.after.length === 0 ? "Its list is left empty: no agent can start in that mode." : "" };
    });
}
