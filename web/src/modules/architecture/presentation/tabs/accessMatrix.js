// The agent × layer access matrix under the Layers tab: a component nested in
// architectureLayersTab, so it edits `draft.layers[i].access` and
// `draft.unlayered` through the page's scope.
//
// Rows are the layers, then the paths in none. Columns are the layer's
// default (for agents it does not list), each agent of the catalog, and plain
// sessions. A cell is a button that cycles none, read, change.

import {
  DEFAULT_ROW,
  PLAIN,
  UNLAYERED,
  accessOf,
  cellLabel,
  matrixAgents,
  nextAccess,
  withAccess,
} from "../../domain/layers.js";

/** @param { gateway: ReturnType<typeof import("../../infrastructure/architecture-gateway.js").architectureGateway> } deps */
export const accessMatrix = ({ gateway }) => () => ({
  /** The agent catalog's names, once discovery answered. */
  catalog: [],

  async init() {
    try {
      this.catalog = await gateway.agents(this.repositoryId);
    } catch {
      this.catalog = []; // the rows fall back to the agents the record names
    }
  },

  /** The agents across the top: the layer's default first, then the catalog's agents, then plain. */
  get columns() {
    const agents = this.draft ? matrixAgents(this.catalog, this.draft) : [PLAIN];
    return [
      { key: DEFAULT_ROW, who: DEFAULT_ROW, label: "Default", note: "agents not listed" },
      ...agents.map((a) => ({ key: a, who: a, label: a, note: a === PLAIN ? "a session with no agent" : "" })),
    ];
  },

  /** The layers down the side, then the unlayered paths. */
  get rows() {
    const columns = this.columns;
    const layers = (this.draft?.layers ?? []).map((l, i) => ({ key: `layer-${i}`, index: i, name: l.name || "unnamed" }));
    return [...layers, { key: UNLAYERED, index: -1, name: UNLAYERED }].map((row) => ({
      key: row.key,
      name: row.name,
      cells: columns.map((c) => {
        const value = this.valueAt(row.index, c.who);
        return { key: `${row.key}|${c.key}`, who: c.who, col: row.index, where: row.name, value, label: cellLabel(c.who, row.name, value) };
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
});
