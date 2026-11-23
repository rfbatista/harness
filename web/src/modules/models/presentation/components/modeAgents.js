// The Mode agents section of the Models page: which agents each session mode
// may start (ADR 0103). One row per mode, read from the server; a row is All
// agents or Selected agents with a checklist of the catalog's agents and the
// "No agent" entry, and a Save of its own. The section follows the catalog
// feed, so a change made on another page or tab shows without a reload.
//
//   <section x-data="modeAgents" data-mode-agents> … <template x-for="v in views"> …

import { describeError } from "../../../../shared/presentation/errors.js";
import { FeedStatus } from "../../../../shared/domain/feed.js";
import {
  ALL, SELECTED, allowsNothing, draftOf, isDirty, merged, toWrite, withAgentToggled, withNoAgentToggled, withState,
} from "../../domain/modeAgents.js";

/**
 * The message is a catalog change: the setting or the agents moved.
 * @param {unknown} data
 */
const isCatalogEvent = (data) => data !== null && typeof data === "object" && "agent_catalog_changed" in data;

/** @param {{ gateway: import("../../domain/ports.js").ModeAgentsGateway }} deps */
export const modeAgents = ({ gateway }) => () => {
  let unfollow = () => {};
  let dropped = false;
  let reading = false;
  let again = false;
  return {
    /** @type {import("../../domain/modeAgents.js").ModeDraft[]} */
    rows: [],
    /** @type {{ name: string, description: string }[]} */
    agents: [],
    loaded: false,
    error: null,
    /** The page feed's status, for whoever watches the section (and the browser tests). */
    feedStatus: "connecting",

    async init() {
      await this.read();
      unfollow = gateway.follow({
        onEvent: (data) => {
          if (isCatalogEvent(data)) void this.read();
        },
        onStatus: (status) => {
          this.feedStatus = status;
          if (status === FeedStatus.PAUSED) dropped = true;
          else if (status === FeedStatus.RESYNCED || (status === FeedStatus.LIVE && dropped)) {
            dropped = false;
            void this.read();
          }
        },
      });
    },
    destroy() {
      unfollow();
    },

    /** Reads the setting and the catalog; a read asked for during one runs once more after it. */
    async read() {
      if (reading) {
        again = true;
        return;
      }
      reading = true;
      try {
        do {
          again = false;
          const [entries, agents] = await Promise.all([gateway.getModeAgents(), gateway.getAgents()]);
          this.agents = agents;
          this.rows = merged(this.rows, entries, agents.map((a) => a.name));
          this.error = null;
        } while (again);
      } catch (err) {
        this.error = describeError(err);
      } finally {
        reading = false;
        this.loaded = true;
      }
    },

    /** What the template draws: each row with its checklist and flags. */
    get views() {
      return this.rows.map((r) => ({
        mode: r.mode,
        name: r.name,
        isAll: r.state === ALL,
        isSelected: r.state === SELECTED,
        dirty: isDirty(r),
        saving: r.saving,
        notice: r.notice,
        empty: allowsNothing(r),
        emptyText: `Nothing can start in ${r.name} mode. Tick an agent or No agent, or switch to All agents.`,
        summary: r.state === ALL ? "Every agent, and a session with no agent." : this.selectedSummary(r),
        saveLabel: r.saving ? "Saving…" : "Save",
        agentRows: this.agents.map((a) => ({ name: a.name, description: a.description, checked: r.agents.includes(a.name), id: `mode-agents-${r.mode || "default"}-${a.name}` })),
        noAgentId: `mode-agents-${r.mode || "default"}-no-agent`,
        noAgentChecked: r.noAgent,
        legend: `${r.name} mode agents`,
      }));
    },
    selectedSummary(r) {
      const parts = [...r.agents];
      if (r.noAgent) parts.push("No agent");
      return parts.length === 0 ? "Nothing." : parts.join(", ");
    },

    /** @param {string} mode @param {(row: any) => any} change */
    update(mode, change) {
      this.rows = this.rows.map((r) => (r.mode === mode ? change(r) : r));
    },
    chooseAll(mode) {
      this.update(mode, (r) => withState(r, ALL));
    },
    chooseSelected(mode) {
      this.update(mode, (r) => withState(r, SELECTED));
    },
    toggleAgent(mode, name) {
      this.update(mode, (r) => withAgentToggled(r, name));
    },
    toggleNoAgent(mode) {
      this.update(mode, (r) => withNoAgentToggled(r));
    },

    async save(mode) {
      const row = this.rows.find((r) => r.mode === mode);
      if (!row || row.saving) return;
      this.update(mode, (r) => ({ ...r, saving: true, notice: "" }));
      this.error = null;
      try {
        const stored = await gateway.setModeAgents(toWrite(row));
        this.update(mode, () => ({ ...draftOf(stored), notice: "Saved." }));
      } catch (err) {
        this.error = describeError(err);
        this.update(mode, (r) => ({ ...r, saving: false }));
      }
    },

    dismissError() {
      this.error = null;
    },
  };
};
