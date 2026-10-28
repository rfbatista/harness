// The enabled agents and MCP servers sections of a project's settings (UX spec
// sections 7 and 9) share one component, so they share one Save, Cancel and
// "Unsaved change" footer. Enabled agents: a switch between All agents and
// Selected agents and, in Selected, a checklist of every catalog agent (a
// search combobox with chips past fifteen). MCP servers: a checklist of every
// catalog server and the What a session gets example. Both name what the
// catalog took off the project and follow the catalog like the Agents page
// does (domain/liveRefresh.js).
//
//   <div x-data="projectMcpServers" data-project-id="…" data-project-name="…">
//
// Save is two calls, set_project_agents then set_project_mcp_servers, and it
// is not atomic: a list that did not change is not written; each call is made
// whatever the other answered, and when one fails after the other succeeded
// the page says which part saved and keeps only the unsaved part dirty.
// Neither verb makes a catalog change to undo, so Undo puts the lists a save
// replaced back with the same verbs.

import { describeError } from "../../../../shared/presentation/errors.js";
import { describeRefusal } from "../../domain/refusals.js";
import { refreshScheduler } from "../../domain/liveRefresh.js";
import { pickerKey } from "../../domain/entryPicker.js";
import {
  ALL, COMBOBOX_PAST, SELECTED, agentPicker, agentRows, agentsChanged, agentsCount, agentsHint, agentsPhrase, agentsSaved, agentsToWrite,
  emptySelection, removedAgents, startSelected,
} from "../../domain/projectAgents.js";
import {
  projectMcpRows, projectMcpSaved, removedFromProject, selectionChanged, sessionExample,
} from "../../domain/projectMcp.js";

const plural = (n, word) => `${n} ${word}${n === 1 ? "" : "s"}`;
const quoted = (names) => names.map((n) => `“${n}”`).join(", ");
const NOTE = "They reach sessions from their next start. Running sessions keep what they started with.";
const LABEL = { agents: "enabled agents", mcp: "MCP servers" };
const cap = (text) => text.charAt(0).toUpperCase() + text.slice(1);
const gone = (names) => `${quoted(names)} ${names.length === 1 ? "was" : "were"} deleted from the catalog and ${names.length === 1 ? "was" : "were"} taken off this project too. The list below is up to date.`;

/**
 * @param {{
 *   gateway: Pick<import("../../domain/ports.js").CatalogGateway, "getCatalog" | "getProjectMcpServers" | "setProjectMcpServers" | "getProjectAgents" | "setProjectAgents"> & Partial<Pick<import("../../domain/ports.js").CatalogGateway, "follow">>,
 *   timers?: { setTimeout: (fn: () => void, ms: number) => any, clearTimeout: (id: any) => void },
 * }} deps
 */
export function projectMcpServers({ gateway, timers = globalThis }) {
  return () => {
    let scheduler = null;
    let unfollow = () => {};
    return {
      projectId: "",
      projectName: "",
      /** @type {import("../../domain/ports.js").Catalog | null} */
      catalog: null,
      /** The MCP list the project has saved, and the one ticked now. */
      saved: /** @type {string[]} */ ([]),
      selected: /** @type {string[]} */ ([]),
      /** The enabled agents the project has saved; the mode and list on the page now (null: no list yet, so Selected starts with every agent ticked). */
      savedAgents: /** @type {import("../../domain/projectAgents.js").ProjectAgents} */ ({ mode: ALL, agents: [] }),
      agentMode: ALL,
      agentTicked: /** @type {string[] | null} */ (null),
      agentQuery: "",
      agentOpen: false,
      agentActive: 0,
      loaded: false,
      saving: false,
      error: null,
      refusal: null,
      agentsRefusal: null,
      /** @type {{ text: string } | null} Which part saved when the other did not. */
      partial: null,
      /** @type {{ names: string[] } | null} */
      removed: null,
      /** @type {{ names: string[] } | null} */
      removedAgentNames: null,
      toast: null,
      feedStatus: "connecting",

      async init() {
        this.projectId = this.$el.dataset.projectId ?? "";
        this.projectName = this.$el.dataset.projectName ?? "";
        await this.load();
        scheduler = refreshScheduler({ reload: () => this.refresh(), timers });
        unfollow = gateway.follow?.({
          onEvent: (data) => scheduler?.event(data),
          onStatus: (status) => {
            this.feedStatus = status;
            scheduler?.status(status);
          },
        }) ?? (() => {});
      },
      destroy() {
        unfollow();
        scheduler?.dispose();
      },
      windowFocused() {
        scheduler?.focus();
      },

      readAll() {
        return Promise.all([gateway.getCatalog(), gateway.getProjectMcpServers(this.projectId), gateway.getProjectAgents(this.projectId)]);
      },
      async load() {
        try {
          const [catalog, names, agents] = await this.readAll();
          this.catalog = catalog;
          this.saved = projectMcpSaved(catalog, names);
          this.selected = [...this.saved];
          this.adoptAgents(agentsSaved(catalog, agents));
          this.error = null;
        } catch (err) {
          this.error = describeError(err);
        } finally {
          this.loaded = true;
        }
      },
      /** The page shows exactly what the project has saved. @param {import("../../domain/projectAgents.js").ProjectAgents} state */
      adoptAgents(state) {
        this.savedAgents = state;
        this.agentMode = state.mode;
        this.agentTicked = state.mode === SELECTED ? [...state.agents] : null;
      },

      /** The catalog or the project's lists changed: read all, keep what was ticked, name what was taken off. */
      async refresh() {
        if (this.saving) return;
        let catalog;
        let names;
        let agents;
        try {
          [catalog, names, agents] = await this.readAll();
        } catch (err) {
          this.error = describeError(err);
          return;
        }
        this.error = null;
        const goneServers = removedFromProject(this.saved, catalog, names);
        const goneAgents = removedAgents(this.savedAgents, catalog, agents);
        const keep = selectionChanged(this.saved, this.selected);
        const next = projectMcpSaved(catalog, names);
        const ticked = projectMcpSaved(catalog, this.selected);
        const keepAgents = this.agentsDirty;
        const mode = this.agentMode;
        const tickedAgents = this.agentTicked;
        if (JSON.stringify(catalog) !== JSON.stringify(this.catalog)) this.catalog = catalog;
        this.saved = next;
        this.selected = keep ? ticked : [...next];
        this.adoptAgents(agentsSaved(catalog, agents));
        if (keepAgents) {
          this.agentMode = mode;
          this.agentTicked = tickedAgents === null ? null : tickedAgents.filter((n) => catalog.agents.some((a) => a.name === n));
        }
        if (goneServers.length > 0) this.removed = { names: [...(this.removed?.names ?? []), ...goneServers.filter((n) => !(this.removed?.names ?? []).includes(n))] };
        if (goneAgents.length > 0) this.removedAgentNames = { names: [...(this.removedAgentNames?.names ?? []), ...goneAgents.filter((n) => !(this.removedAgentNames?.names ?? []).includes(n))] };
      },

      // What the sections show: enabled agents.

      get agentRows() {
        return this.catalog ? agentRows(this.catalog, this.agentTicked ?? []) : [];
      },
      get hasAgentRows() {
        return this.agentRows.length > 0;
      },
      get isSelectedMode() {
        return this.agentMode === SELECTED;
      },
      get isAllMode() {
        return this.agentMode === ALL;
      },
      get showAgentChecklist() {
        return this.isSelectedMode && this.hasAgentRows && this.agentRows.length <= COMBOBOX_PAST;
      },
      get showAgentCombobox() {
        return this.isSelectedMode && this.agentRows.length > COMBOBOX_PAST;
      },
      get agentsHint() {
        return agentsHint(this.agentMode);
      },
      get agentsCount() {
        return this.catalog ? agentsCount(this.agentMode, this.agentTicked, this.agentRows.length) : "";
      },
      get showEmptyAgents() {
        return this.loaded && this.catalog !== null && emptySelection(this.agentMode, this.agentTicked);
      },
      get tickAllLabel() {
        return `Tick all ${this.agentRows.length}`;
      },
      get agentsDirty() {
        return agentsChanged(this.savedAgents, this.agentMode, this.agentTicked);
      },
      get agentsDescribedBy() {
        return this.agentsRefusal ? "settings-agents-hint settings-agents-refusal" : "settings-agents-hint";
      },
      get hasAgentBanner() {
        return this.removedAgentNames !== null;
      },
      get agentBannerText() {
        const names = this.removedAgentNames?.names ?? [];
        return names.length === 0 ? "" : gone(names);
      },
      /** The combobox: unticked agents matching the query, the keys of the options and chips. */
      get agentPick() {
        const found = this.catalog ? agentPicker(this.catalog, this.agentTicked ?? [], this.agentQuery) : { options: [], hidden: 0, empty: true, chips: [] };
        const at = found.options.length ? Math.min(this.agentActive, found.options.length - 1) : 0;
        const query = this.agentQuery.trim();
        return {
          expanded: this.agentOpen,
          activeId: this.agentOpen && found.options.length ? `agent-opt-${at}` : null,
          options: found.options.map((o, i) => ({ ...o, id: `agent-opt-${i}`, active: this.agentOpen && i === at })),
          hiddenNote: found.hidden ? `${found.hidden} more: keep typing to narrow the list` : "",
          hasHidden: found.hidden > 0,
          empty: found.empty,
          emptyNote: query ? `No agent matches “${query}”.` : "Every agent is ticked.",
          chips: found.chips.map((name) => ({ name, removeLabel: `Remove ${name}` })),
          noChips: found.chips.length === 0,
        };
      },

      // What the sections show: MCP servers.

      get hasCatalog() {
        return this.catalog !== null;
      },
      get rows() {
        return this.catalog ? projectMcpRows(this.catalog, this.projectId, this.selected) : [];
      },
      get isEmpty() {
        return this.loaded && this.catalog !== null && this.catalog.mcpServers.length === 0;
      },
      get hasRows() {
        return this.rows.length > 0;
      },
      get isLoading() {
        return !this.loaded;
      },
      get mcpChanged() {
        return selectionChanged(this.saved, this.selected);
      },
      /** Either list differs from what is saved. */
      get changed() {
        return this.mcpChanged || this.agentsDirty;
      },
      get unchanged() {
        return !this.changed;
      },
      get count() {
        return this.hasRows ? `${this.selected.length} of ${this.rows.length} selected` : "";
      },
      get brokenWhy() {
        const b = this.catalog?.broken ?? null;
        return b ? `${b.path} does not parse` : "";
      },
      get isBroken() {
        return this.brokenWhy !== "";
      },
      get brokenNote() {
        return this.isBroken ? `${this.brokenWhy}, so saving is off until the file is fixed by hand. The lists below are the last version that parsed.` : "";
      },
      get cannotSave() {
        return !this.changed || this.saving || this.isBroken;
      },
      get saveLabel() {
        return this.saving ? "Saving…" : "Save";
      },
      get mcpDescribedBy() {
        return this.refusal ? "settings-mcp-refusal" : null;
      },
      get example() {
        const ex = this.catalog ? sessionExample(this.catalog, this.selected) : { agent: "", withAgent: [], withoutAgent: [] };
        return {
          ...ex,
          hasAgent: ex.agent !== "",
          asAgent: `As ${ex.agent}`,
          noneWithAgent: ex.withAgent.length === 0,
          noneWithoutAgent: ex.withoutAgent.length === 0,
        };
      },
      get bannerText() {
        const names = this.removed?.names ?? [];
        return names.length === 0 ? "" : gone(names);
      },
      get hasBanner() {
        return this.removed !== null;
      },
      get hasToast() {
        return this.toast !== null;
      },
      get hasPartial() {
        return this.partial !== null;
      },

      // What the person does.

      /** @param {string} name */
      toggle(name) {
        this.refusal = null;
        this.partial = null;
        this.selected = this.selected.includes(name) ? this.selected.filter((n) => n !== name) : [...this.selected, name];
      },
      /** @param {"all" | "selected"} mode */
      setMode(mode) {
        this.agentsRefusal = null;
        this.partial = null;
        if (mode === SELECTED && this.catalog) this.agentTicked = startSelected(this.catalog, this.agentTicked);
        this.agentMode = mode;
      },
      chooseAll() {
        this.setMode(ALL);
      },
      chooseSelected() {
        this.setMode(SELECTED);
      },
      /** @param {string} name */
      toggleAgent(name) {
        this.agentsRefusal = null;
        this.partial = null;
        const now = this.agentTicked ?? [];
        this.agentTicked = now.includes(name) ? now.filter((n) => n !== name) : [...now, name];
      },
      tickAll() {
        this.agentsRefusal = null;
        this.agentTicked = this.catalog ? this.catalog.agents.map((a) => a.name) : [];
      },
      // The combobox past fifteen agents.
      agentFocus() {
        this.agentOpen = true;
      },
      agentBlur() {
        this.agentOpen = false;
      },
      agentInput() {
        this.agentOpen = true;
        this.agentActive = 0;
      },
      /** @param {KeyboardEvent} event */
      agentKey(event) {
        const v = this.agentPick;
        const r = pickerKey({ open: this.agentOpen, active: this.agentActive, query: this.agentQuery, chosen: v.chips.map((c) => c.name) }, event.key, v.options.map((o) => o.key));
        if (!r.handled) return;
        event.preventDefault();
        this.agentOpen = r.open;
        this.agentActive = r.active;
        if (r.add !== undefined) {
          this.toggleAgent(r.add);
          this.agentQuery = "";
        }
        if (r.removeLast) this.toggleAgent(v.chips[v.chips.length - 1].name);
      },
      /** A mouse press on an option; it keeps the focus in the input. @param {MouseEvent} event */
      agentPickOption(event) {
        event.preventDefault();
        this.toggleAgent(/** @type {HTMLElement} */ (event.currentTarget).dataset.key ?? "");
        this.agentQuery = "";
      },
      /** @param {MouseEvent} event */
      agentUnpick(event) {
        this.toggleAgent(/** @type {HTMLElement} */ (event.currentTarget).dataset.key ?? "");
      },
      cancel() {
        this.refusal = null;
        this.agentsRefusal = null;
        this.partial = null;
        this.selected = [...this.saved];
        this.adoptAgents(this.savedAgents);
      },
      dismissBanner() {
        this.removed = null;
      },
      dismissAgentBanner() {
        this.removedAgentNames = null;
      },
      dismissToast() {
        this.toast = null;
      },

      async save() {
        if (this.cannotSave) return;
        const parts = {};
        if (this.agentsDirty && this.catalog) parts.agents = agentsToWrite(this.catalog, this.agentMode, this.agentTicked);
        if (this.mcpChanged) parts.mcp = [...this.selected];
        await this.write(parts, "saved");
      },
      async undoSave() {
        const before = this.toast?.undoTo;
        if (!before) return;
        await this.write(before, "undone");
      },

      /**
       * Writes the parts given, agents first, each whatever the other answered.
       * @param {{ agents?: import("../../domain/projectAgents.js").ProjectAgents, mcp?: string[] }} parts
       * @param {"saved"|"undone"} kind
       */
      async write(parts, kind) {
        const previous = { agents: this.savedAgents, mcp: this.saved };
        const done = /** @type {string[]} */ ([]);
        const failed = /** @type {{ part: string, refusal: any }[]} */ ([]);
        this.saving = true;
        this.refusal = null;
        this.agentsRefusal = null;
        this.partial = null;
        try {
          if (parts.agents) {
            try {
              const answer = await gateway.setProjectAgents(this.projectId, parts.agents);
              this.adoptAgents(agentsSaved(this.catalog, answer));
              done.push("agents");
            } catch (err) {
              const r = describeRefusal(err);
              this.agentsRefusal = r;
              failed.push({ part: "agents", refusal: r });
              if (r.code === "UNKNOWN_REFERENCE") scheduler?.now();
            }
          }
          if (parts.mcp) {
            try {
              const answer = await gateway.setProjectMcpServers(this.projectId, parts.mcp);
              this.saved = projectMcpSaved(this.catalog, answer);
              this.selected = [...this.saved];
              done.push("mcp");
            } catch (err) {
              const r = describeRefusal(err);
              this.refusal = r;
              failed.push({ part: "mcp", refusal: r });
              if (r.code === "UNKNOWN_REFERENCE") scheduler?.now();
            }
          }
          if (done.length > 0 && failed.length > 0) {
            const sentence = `${cap(done.map((p) => LABEL[p]).join(" and "))} were ${kind === "saved" ? "saved" : "put back"}. ${cap(failed.map((f) => LABEL[f.part]).join(" and "))} were not: ${failed[0].refusal.message}`;
            this.partial = { text: sentence };
          }
          this.toast = this.toastFor(kind, done, failed, previous);
        } finally {
          this.saving = false;
        }
      },

      /** What the person is told once the calls answered. */
      toastFor(kind, done, failed, previous) {
        if (done.length === 0) {
          // Nothing was written: a save shows its refusals at the lists, an Undo says so here.
          return kind === "undone" ? { text: failed[0].refusal.message, note: "", tone: "danger", undoTo: null, hasUndo: false } : this.toast;
        }
        const phrases = done.map((p) => (p === "agents" ? agentsPhrase(this.savedAgents) : `gives ${plural(this.saved.length, "MCP server")}${kind === "saved" ? " to its sessions" : ""}`));
        const text = kind === "saved" ? `Saved — ${this.projectName} now ${phrases.join(" and ")}.` : `Undone — ${this.projectName} ${phrases.join(" and ")} again.`;
        if (kind === "undone") return { text, note: "Running sessions keep what they started with.", tone: "", undoTo: null, hasUndo: false };
        const undoTo = {};
        if (done.includes("agents")) undoTo.agents = previous.agents;
        if (done.includes("mcp")) undoTo.mcp = previous.mcp;
        return { text, note: done.includes("mcp") ? NOTE : "Running sessions keep what they started with.", tone: "", undoTo, hasUndo: true };
      },
    };
  };
}
