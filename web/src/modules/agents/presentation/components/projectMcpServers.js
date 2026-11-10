// The enabled agents and MCP servers sections of a project's settings (UX spec
// sections 7 and 9) share one component, so they share one Save, Cancel and
// "Unsaved change" footer. Enabled agents: a switch between All agents and
// Selected agents and, in Selected, a checklist of every catalog agent (a
// search combobox with chips past fifteen). MCP servers: a checklist of every
// catalog server, each with its project MCP state (Added, Agent's choice or
// Banned) and, under a server whose tools are known, a tool list (Default,
// Enabled or Disabled each), then the What a session gets example. Both name
// what the catalog took off the project and follow the catalog like the Agents
// page does (domain/liveRefresh.js).
//
//   <div x-data="projectMcpServers" data-project-id="…" data-project-name="…">
//
// Save is two calls, set_project_agents then set_project_mcp_settings, and it
// is not atomic: a list that did not change is not written; each call is made
// whatever the other answered, and when one fails after the other succeeded
// the page says which part saved and keeps only the unsaved part dirty.
// Neither verb makes a catalog change to undo, so Undo puts the lists a save
// replaced back with the same verbs. Refresh tools probes one server and takes
// its new tool list; the states and overrides the person has not saved stay.

import { noOwnServers } from "../../domain/mcp.js";
import { describeError } from "../../../../shared/presentation/errors.js";
import { followSkeleton, skeletonFields } from "../../../../shared/presentation/skeleton.js";
import { describeRefusal } from "../../domain/refusals.js";
import { refreshScheduler } from "../../domain/liveRefresh.js";
import { pickerKey } from "../../domain/entryPicker.js";
import {
  ALL, COMBOBOX_PAST, SELECTED, agentPicker, agentRows, agentsChanged, agentsCount, agentsHint, agentsPhrase, agentsSaved, agentsToWrite,
  emptySelection, removedAgents, startSelected,
} from "../../domain/projectAgents.js";
import { probeOutcome } from "../../domain/mcpForm.js";
import {
  addedNames, mergeSettings, namesInState, projectMcpRows, removedFromProject, resetServerOverrides, sessionExample, setNames, setServerState,
  setToolOverride, settingsChanged, settingsCount, settingsDraft, settingsPhrase, settingsToWrite,
} from "../../domain/projectMcp.js";

const quoted = (names) => names.map((n) => `“${n}”`).join(", ");
const NOTE = "They reach sessions from their next start. Running sessions keep what they started with.";
const LABEL = { agents: "enabled agents", mcp: "MCP servers" };
const cap = (text) => text.charAt(0).toUpperCase() + text.slice(1);
const gone = (names) => `${quoted(names)} ${names.length === 1 ? "was" : "were"} deleted from the catalog and ${names.length === 1 ? "was" : "were"} taken off this project too. The list below is up to date.`;

/**
 * @param {{
 *   gateway: Pick<import("../../domain/ports.js").CatalogGateway, "getCatalog" | "getProjectMcpSettings" | "setProjectMcpSettings" | "refreshMcpServerTools" | "getProjectAgents" | "setProjectAgents"> & Partial<Pick<import("../../domain/ports.js").CatalogGateway, "follow">>,
 *   timers?: { setTimeout: (fn: () => void, ms: number) => any, clearTimeout: (id: any) => void },
 * }} deps
 */
export function projectMcpServers({ gateway, timers = globalThis }) {
  return () => {
    let scheduler = null;
    let unfollow = () => {};
    /** When the first read shows a skeleton (shared rule). */
    let skeleton = null;
    return {
      ...skeletonFields(),
      projectId: "",
      projectName: "",
      /** @type {import("../../domain/ports.js").Catalog | null} */
      catalog: null,
      /** The MCP settings the project has saved, and the states and overrides on the page now. */
      settings: /** @type {import("../../domain/ports.js").ProjectMcpSettings} */ ({ added: [], servers: [] }),
      draft: /** @type {import("../../domain/projectMcp.js").McpDraft} */ ({}),
      /** The server Refresh tools is probing ("" when none), and what each refresh found. */
      refreshingName: "",
      refreshNotes: /** @type {Record<string, { show: boolean, tone: string, text: string }>} */ ({}),
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
        skeleton = followSkeleton(this);
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
        skeleton?.stop();
      },
      windowFocused() {
        scheduler?.focus();
      },

      readAll() {
        return Promise.all([gateway.getCatalog(), gateway.getProjectMcpSettings(this.projectId), gateway.getProjectAgents(this.projectId)]);
      },
      async load() {
        if (!this.loaded) skeleton?.begin();
        try {
          const [catalog, settings, agents] = await this.readAll();
          // The lists come in once the skeleton is gone, so they never stack under it.
          await skeleton?.end();
          this.catalog = catalog;
          this.settings = settings;
          this.draft = settingsDraft(settings);
          this.adoptAgents(agentsSaved(catalog, agents));
          this.error = null;
        } catch (err) {
          skeleton?.fail();
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
        let settings;
        let agents;
        try {
          [catalog, settings, agents] = await this.readAll();
        } catch (err) {
          this.error = describeError(err);
          return;
        }
        this.error = null;
        const goneServers = removedFromProject(setNames(this.settings), catalog, setNames(settings));
        const goneAgents = removedAgents(this.savedAgents, catalog, agents);
        const keepAgents = this.agentsDirty;
        const mode = this.agentMode;
        const tickedAgents = this.agentTicked;
        if (JSON.stringify(catalog) !== JSON.stringify(this.catalog)) this.catalog = catalog;
        this.draft = mergeSettings(this.settings, this.draft, settings);
        this.settings = settings;
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
      /** The Added servers saved, and the ones Added on the page now. */
      get saved() {
        return addedNames(this.settings);
      },
      get selected() {
        return namesInState(this.settings, this.draft, "added");
      },
      get rows() {
        if (!this.catalog) return [];
        return projectMcpRows(this.catalog, this.projectId, this.settings, this.draft).map((r) => {
          const note = this.refreshNotes[r.name] ?? { show: false, tone: "", text: "" };
          const busy = this.refreshingName === r.name;
          return { ...r, refreshBusy: String(busy), refreshDisabled: this.refreshingName !== "" || this.saving, refreshLabel: busy ? "Refreshing…" : "Refresh tools", refreshNote: note.text, hasRefreshNote: note.show, refreshTone: note.tone };
        });
      },
      get isEmpty() {
        return this.loaded && this.catalog !== null && noOwnServers(this.catalog);
      },
      get hasRows() {
        return this.rows.length > 0;
      },
      get isLoading() {
        return !this.loaded;
      },
      get mcpChanged() {
        return settingsChanged(this.settings, this.draft);
      },
      /** Either list differs from what is saved. */
      get changed() {
        return this.mcpChanged || this.agentsDirty;
      },
      get unchanged() {
        return !this.changed;
      },
      get count() {
        return this.hasRows ? settingsCount(this.settings, this.draft) : "";
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
        const ex = this.catalog ? sessionExample(this.catalog, this.settings, this.draft) : { agent: "", withAgent: [], withoutAgent: [], banned: [] };
        return {
          ...ex,
          hasBanned: ex.banned.length > 0,
          bannedText: `Banned in this project, so no session gets ${ex.banned.length === 1 ? "it" : "them"}: ${ex.banned.join(", ")}`,
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

      /** A server's state button: data-name and data-state. @param {MouseEvent} event */
      pickState(event) {
        const { name, state } = /** @type {HTMLElement} */ (event.currentTarget).dataset;
        this.setState(name ?? "", state ?? "");
      },
      /** @param {string} name @param {string} state added, agent or banned */
      setState(name, state) {
        this.refusal = null;
        this.partial = null;
        this.draft = setServerState(this.draft, name, /** @type {any} */ (state));
      },
      /** A tool's Default, Enabled or Disabled button: data-name, data-tool and data-choice. @param {MouseEvent} event */
      pickTool(event) {
        const { name, tool, choice } = /** @type {HTMLElement} */ (event.currentTarget).dataset;
        this.setTool(name ?? "", tool ?? "", choice ?? "");
      },
      /** @param {string} name @param {string} tool @param {string} choice default, enabled or disabled */
      setTool(name, tool, choice) {
        this.refusal = null;
        this.partial = null;
        this.draft = setToolOverride(this.draft, name, tool, /** @type {any} */ (choice));
      },
      /** Reset to defaults: every tool override of one server goes back to Default. @param {MouseEvent} event */
      resetTools(event) {
        this.refusal = null;
        this.partial = null;
        this.draft = resetServerOverrides(this.draft, /** @type {HTMLElement} */ (event.currentTarget).dataset.name ?? "");
      },
      /** Refresh tools: probes the server, then reads the settings again; what the person changed and has not saved stays. @param {MouseEvent} event */
      async refreshTools(event) {
        const name = /** @type {HTMLElement} */ (event.currentTarget).dataset.name ?? "";
        if (this.refreshingName !== "" || this.saving) return;
        this.refreshingName = name;
        this.refreshNotes = { ...this.refreshNotes, [name]: { show: false, tone: "", text: "" } };
        let note;
        try {
          const { server, probe } = await gateway.refreshMcpServerTools(name);
          const next = await gateway.getProjectMcpSettings(this.projectId);
          this.draft = mergeSettings(this.settings, this.draft, next);
          this.settings = next;
          note = probeOutcome(probe, { tools: server.knownTools });
        } catch (err) {
          note = { show: true, tone: "attention", text: describeError(err).message };
        }
        this.refreshNotes = { ...this.refreshNotes, [name]: note };
        this.refreshingName = "";
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
        this.draft = settingsDraft(this.settings);
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
        if (this.mcpChanged) parts.mcp = settingsToWrite(this.settings, this.draft);
        await this.write(parts, "saved");
      },
      async undoSave() {
        const before = this.toast?.undoTo;
        if (!before) return;
        await this.write(before, "undone");
      },

      /**
       * Writes the parts given, agents first, each whatever the other answered.
       * @param {{ agents?: import("../../domain/projectAgents.js").ProjectAgents, mcp?: ReturnType<typeof settingsToWrite> }} parts
       * @param {"saved"|"undone"} kind
       */
      async write(parts, kind) {
        const previous = { agents: this.savedAgents, mcp: settingsToWrite(this.settings, settingsDraft(this.settings)) };
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
              const answer = await gateway.setProjectMcpSettings(this.projectId, parts.mcp);
              this.settings = answer;
              this.draft = settingsDraft(answer);
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
        const phrases = done.map((p) => (p === "agents" ? agentsPhrase(this.savedAgents) : settingsPhrase(this.settings, kind)));
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
