// What the tabs that name agents share: the agent catalog (fetched once, shared
// through the gateway) and the picking of names from it. Spread the result into
// a tab's component; `this` is then the tab.

import { agentChoices, agentSummary, toggled } from "../../domain/descriptive.js";
import { agentSlot } from "../../domain/layers.js";

/** @param { gateway: ReturnType<typeof import("../../infrastructure/architecture-gateway.js").architectureGateway> } deps */
export const agentPicker = ({ gateway }) => ({
  /** The agent catalog's names, once discovery answered. */
  catalog: [],
  /** Catalog agents the project does not enable: offered to no one here, marked when the record keeps one. */
  notEnabled: [],

  async init() {
    try {
      [this.catalog, this.notEnabled] = await Promise.all([gateway.agents(this.repositoryId, this.projectId), gateway.notEnabledAgents(this.repositoryId, this.projectId)]);
    } catch {
      this.catalog = []; // the pickers fall back to the names the record holds
      this.notEnabled = [];
    }
  },

  /** The choices for an agent field of a row: a list field gets "all" too. */
  agentOptions(row, field, withAll) {
    const v = row[field];
    return agentChoices(this.catalog, Array.isArray(v) ? v : v ? [v] : [], withAll, this.notEnabled);
  },
  /** The colour slot of an agent's chip. */
  slotOf(name) {
    return agentSlot(name);
  },
  agentSummary(row, field) {
    return agentSummary(Array.isArray(row[field]) ? row[field] : []);
  },
  /** A checkbox of a list field was ticked or unticked. */
  toggleAgent(row, field, name, event) {
    row[field] = toggled(Array.isArray(row[field]) ? row[field] : [], name, event.target.checked);
  },
  /** A single agent field's select changed. */
  setAgent(row, field, event) {
    row[field] = event.target.value;
  },
});
