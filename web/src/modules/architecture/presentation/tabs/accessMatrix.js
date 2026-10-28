// The agent × layer access matrix under the Layers tab: a component nested in
// architectureLayersTab, so it edits `draft.layers[i].access` and
// `draft.unlayered` through the page's scope and reads which layer is open.
//
// Rows are the agents: the default (for agents a layer does not list), each
// agent of the catalog, and plain sessions. Columns are the layers, then the
// paths in none. A cell is a button that cycles none, read, change; an agent's
// name sets its whole row.

import {
  DEFAULT_ROW,
  PLAIN,
  UNLAYERED,
  accessOf,
  agentSlot,
  cellLabel,
  matrixAgents,
  nextAccess,
  withAccess,
  withRowAccess,
} from "../../domain/layers.js";

/** @param { gateway: ReturnType<typeof import("../../infrastructure/architecture-gateway.js").architectureGateway> } deps */
export const accessMatrix = ({ gateway }) => () => ({
  /** The agent catalog's names, once discovery answered. */
  catalog: [],

  async init() {
    try {
      this.catalog = await gateway.agents(this.repositoryId, this.projectId);
    } catch {
      this.catalog = []; // the rows fall back to the agents the record names
    }
  },

  /** The columns: each layer, then the paths in none. */
  get columns() {
    const open = this.selectedIndex;
    const layers = (this.draft?.layers ?? []).map((l, i) => {
      const name = l.name || "unnamed";
      return { key: `layer-${i}`, index: i, name, label: name, selected: i === open };
    });
    return [...layers, { key: UNLAYERED, index: -1, name: UNLAYERED, label: UNLAYERED, selected: false }];
  },

  /** The rows: the default first, then the catalog's agents, then plain. */
  get rows() {
    const columns = this.columns;
    const agents = this.draft ? matrixAgents(this.catalog, this.draft) : [PLAIN];
    const open = this.draft?.layers[this.selectedIndex]?.name || "the open layer";
    const who = [
      { who: DEFAULT_ROW, label: "Default", note: "agents not listed" },
      ...agents.map((a) => ({ who: a, label: a, note: a === PLAIN ? "a session with no agent" : "" })),
    ];
    return who.map((r) => ({
      key: r.who,
      who: r.who,
      label: r.label,
      note: r.note,
      slot: agentSlot(r.who),
      setLabel: `Set ${r.label} on every layer to its access on ${open}`,
      cells: columns.map((c) => {
        const value = this.valueAt(c.index, r.who);
        return { key: `${r.who}|${c.key}`, who: r.who, col: c.index, where: c.name, selected: c.selected, value, label: cellLabel(r.who, c.name, value) };
      }),
    }));
  },

  /** The access an agent (or the default) has on a layer, or on the unlayered paths with index -1. */
  valueAt(index, who) {
    const access = index === -1 ? this.draft?.unlayered : this.draft?.layers[index]?.access;
    return who === DEFAULT_ROW ? (access?.default ?? "read") : accessOf(access, who);
  },

  /** Moves a cell to the next level: none, read, change, none… */
  press(index, who) {
    const next = nextAccess(this.valueAt(index, who));
    if (index === -1) {
      this.draft.unlayered = withAccess(this.draft.unlayered, who, next);
    } else {
      this.draft.layers[index].access = withAccess(this.draft.layers[index].access, who, next);
    }
  },

  /** Gives an agent (or the default) on every layer the access it has on the open one. */
  setRow(who) {
    if (!this.draft || this.draft.layers.length === 0) return;
    this.draft.layers = withRowAccess(this.draft.layers, who, this.valueAt(this.selectedIndex, who));
  },
});
