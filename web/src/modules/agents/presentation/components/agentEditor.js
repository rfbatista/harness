// The agent editor, a full page at /agents/new and /agents/<key>/edit: the
// sticky contents list, Identity (name, alias chips, required description),
// Skills (the combobox over skills and bundles with its chips and the summary
// of what they resolve to), MCP servers, Allowed tools, and the save
// pipeline (preview, Review change, conflict, toast, leave guard) around
// them. Mixed into the Agents page, which provides the catalog, the route
// (tab, item, mode), `load()`, `navigateTo()` and the toast.
//
// The page never builds file bytes: the server's dry run is the preview. It
// computes locally only what the form needs per keystroke: the slug check,
// the description rule and the resolved skills of the chips.
//
// Where a member has the same name as the bundle editor's (sv, fieldErrors,
// saveEditor …), editors.js picks the one of the editor that is open.

import {
  CREATE_SKILL, agentChanges, agentDraft, agentFieldErrors, agentPickerOptions, checkAgentDraft, createSkillHref,
  mcpChoices, resolvedSummary, tocCounts, toolChoices,
} from "../../domain/agentDraft.js";
import { projectChoices, projectsToSend, toggleProject } from "../../domain/enabledIn.js";
import { isMcpName, isSlug } from "../../domain/names.js";
import { openSave, saveView, savedToast } from "../../domain/savePipeline.js";
import { entryPickerMixin } from "./entryPickerMixin.js";
import { settleEditor } from "./liveCatalog.js";
import { saveController } from "./saveController.js";

const enc = encodeURIComponent;
const FILES = ["agents.json"];
const NO_OPTIONS = { options: [], hidden: 0, empty: true };
const STASH = "harness.agent-editor";
/** A half-done form is kept this long while the person makes a skill. */
const STASH_LIFE_MS = 60 * 60 * 1000;

/** The versions of the files this editor writes, as the catalog read them. */
const versionsOf = (catalog) => Object.fromEntries(FILES.filter((f) => f in catalog.versions).map((f) => [f, catalog.versions[f]]));

/**
 * @param {{
 *   gateway: Pick<import("../../domain/ports.js").CatalogGateway, "saveAgent">,
 *   location: { pathname: string, search?: string, hash?: string, assign?: (href: string) => void },
 *   history?: { replaceState?: (state: any, title: string, url: string) => void },
 *   storage?: Pick<Storage, "getItem" | "setItem" | "removeItem"> | null,
 *   timers?: { setTimeout: (fn: () => void, ms: number) => any, clearTimeout: (id: any) => void },
 * }} deps
 */
export function agentEditor({ gateway, location, history, storage = globalThis.sessionStorage ?? null, timers }) {
  /** @type {any} */
  let page;
  /** @type {ReturnType<typeof saveController>} */
  let saver;

  const picker = entryPickerMixin("skillPicker", {
    options: (p, query) => (p.catalog && p.agentDraft ? agentPickerOptions(p.catalog, { query, entries: p.agentDraft.entries }) : NO_OPTIONS),
    chips: (p) => {
      if (!p.agentDraft) return [];
      const skills = new Set(p.catalog.skills.map((s) => s.name));
      const bundles = new Map(p.catalog.bundles.map((b) => [b.name, b]));
      return p.agentDraft.entries.map((e) => {
        if (e.startsWith("@")) {
          const b = bundles.get(e.slice(1));
          return b ? { key: e, label: `${e} (${b.resolvedSkills.length})`, kind: "bundle", missing: false, removeLabel: `Remove ${e}` } : { key: e, label: `${e} · missing`, kind: "bundle", missing: true, removeLabel: `Remove ${e}, it is missing` };
        }
        const missing = !skills.has(e);
        return { key: e, label: missing ? `${e} · missing` : e, kind: "skill", missing, removeLabel: missing ? `Remove ${e}, it is missing` : `Remove ${e}` };
      });
    },
    add: (p, key) => {
      if (!p.agentDraft) return;
      if (key === CREATE_SKILL) {
        p.createSkill(p.skillPickerQuery);
        return;
      }
      if (p.agentDraft.entries.includes(key)) return;
      p.agentDraft.entries.push(key);
      p.agentChanged();
    },
    remove: (p, key) => {
      if (!p.agentDraft) return;
      p.agentDraft.entries = p.agentDraft.entries.filter((e) => e !== key);
      p.agentChanged();
    },
  });

  const editor = {
    /** The form, or null when no agent editor is open. */
    agentDraft: null,
    agentSave: openSave({}),
    /** Whether the person has changed the form: an untouched new form does not scold. */
    agentTouched: false,
    aliasQuery: "",
    aliasError: "",
    /** The contents list's current section. */
    tocOn: "identity",

    setupEditor() {
      page = this;
      saver = saveController({
        run: (options) => gateway.saveAgent({ previousName: page.agentDraft.previousName || undefined, agent: { name: page.agentDraft.name, description: page.agentDraft.description.trim(), aliases: [...page.agentDraft.aliases], entries: [...page.agentDraft.entries], mcpServers: [...page.agentDraft.mcpServers], allowedTools: [...page.agentDraft.allowedTools] }, projects: projectsToSend(page.catalog, page.agentDraft) }, options),
        onState: (state) => { page.agentSave = state; },
        blocked: () => page.brokenWhy !== "",
        timers,
      });
    },

    /** Opens, or closes, the editor the route names. */
    syncEditor() {
      this.wantProjects?.();
      const wanted = this.catalog !== null && this.tab === "agents" && this.mode !== "";
      if (!wanted) {
        if (this.agentDraft !== null) this.closeEditor();
        return;
      }
      const params = new URLSearchParams(location.search ?? "");
      const draft = agentDraft(this.catalog, this.mode === "edit" ? this.item : "", params.get("from") ?? "");
      if (!draft) {
        if (this.agentDraft !== null) this.closeEditor();
        return;
      }
      this.agentDraft = draft;
      this.agentTouched = false;
      this.reviewOpen = false;
      this.skillPickerQuery = "";
      this.aliasQuery = "";
      this.aliasError = "";
      this.departing = false;
      saver.open(versionsOf(this.catalog));
      const back = this.takeStash(params.get("added") ?? "");
      if (back || (draft.entries.length > 0 && draft.previousName === "")) this.agentChanged();
      this.$nextTick?.(() => this.landOnSection());
    },

    closeEditor() {
      // The stash is this editor's own half-done form. Another form closing (the skill form, which every
      // editor shares closeEditor with) must leave it for the editor the person is going back to.
      if (this.agentDraft !== null) {
        try {
          storage?.removeItem(STASH);
        } catch {
          // Nothing kept, nothing to drop.
        }
      }
      saver.open({});
      this.agentDraft = null;
      this.agentTouched = false;
      this.reviewOpen = false;
      this.leaving = null;
    },

    /** Scrolls to the section the address names (a Change link), and puts the keyboard on a missing description. */
    landOnSection() {
      const id = (location.hash ?? "").replace(/^#/, "");
      if (!id) return;
      const section = document.getElementById(id);
      section?.scrollIntoView?.();
      this.tocOn = id;
      if (id === "identity" && this.agentDraft?.description.trim() === "") document.getElementById("agent-desc")?.focus();
    },

    // The trip to the skill form and back.

    /** Where the skill form sends the person back to: this editor, at the skills. */
    get returnPath() {
      const params = new URLSearchParams(location.search ?? "");
      params.delete("added");
      const search = params.toString();
      return `${location.pathname}${search ? `?${search}` : ""}#skills`;
    },
    /** This address without the `added` the skill form appended. */
    get cleanUrl() {
      const params = new URLSearchParams(location.search ?? "");
      params.delete("added");
      const search = params.toString();
      return `${location.pathname}${search ? `?${search}` : ""}${location.hash ?? ""}`;
    },
    get stashKey() {
      return `${location.pathname}${new URLSearchParams(location.search ?? "").get("from") ? "?from" : ""}`;
    },
    /** Keeps the half-done form while the person is on another form. */
    stashDraft() {
      if (!this.agentDraft) return;
      try {
        storage?.setItem(STASH, JSON.stringify({ key: this.stashKey, at: Date.now(), draft: this.agentDraft, touched: this.agentTouched }));
      } catch {
        // No storage: the trip loses the edit.
      }
      this.leaving = null;
      this.departing = true;
    },
    /** The skill form, to make the skill the person looked for; it sends them back here. */
    createSkill(query) {
      this.stashDraft();
      location.assign?.(createSkillHref(this.returnPath, query));
    },
    /** The MCP server form (a drawer, once it exists), which sends the person back here. */
    get newMcpHref() {
      return `/agents/mcp/new?return=${enc(this.returnPath.replace(/#skills$/, "#mcp"))}`;
    },
    /** Restores what stashDraft kept, with the new skill chosen. Returns whether anything came back. */
    takeStash(added) {
      let kept = null;
      try {
        kept = JSON.parse(storage?.getItem(STASH) ?? "null");
        storage?.removeItem(STASH);
      } catch {
        kept = null;
      }
      const mine = kept !== null && kept.key === this.stashKey && Date.now() - kept.at < STASH_LIFE_MS && kept.draft?.previousName === this.agentDraft.previousName;
      if (mine) {
        this.agentDraft = { ...kept.draft };
        this.agentTouched = Boolean(kept.touched);
      }
      if (added) {
        // Back from the skill form the address ends in #skills; back from the MCP server form, in #mcp.
        if (/#mcp$/.test(location.hash ?? "")) this.addServer(added);
        else this.addEntry(added);
        history?.replaceState?.({}, "", this.cleanUrl);
      }
      return mine || added !== "";
    },
    addServer(name) {
      if (isMcpName(name) && !this.agentDraft.mcpServers.includes(name)) this.agentDraft.mcpServers.push(name);
    },
    addEntry(key) {
      if (isSlug(key) && !this.agentDraft.entries.includes(key)) this.agentDraft.entries.push(key);
    },

    get agentEditing() {
      return this.agentDraft !== null;
    },
    get agentTitle() {
      return this.agentDraft?.previousName ? `Edit ${this.agentDraft.previousName}` : "New agent";
    },
    get agentPath() {
      return this.agentDraft?.previousName ? `agents.json › agents.${this.agentDraft.previousName}` : "will be added to agents.json › agents";
    },
    get editorHref() {
      return this.agentDraft?.previousName ? `/agents/${enc(this.agentDraft.previousName)}/edit` : "/agents/new";
    },
    get saveButtonLabel() {
      if (this.sv.saving) return "Saving…";
      return this.agentDraft?.previousName ? "Save agent" : "Create agent";
    },

    get agentCheck() {
      return this.agentDraft ? checkAgentDraft(this.agentDraft, { touched: this.agentTouched || this.agentDraft.previousName !== "" }) : { valid: false, nameProblem: "", descriptionProblem: "" };
    },
    get agentChangeCount() {
      return this.agentDraft && this.catalog ? agentChanges(this.catalog, this.agentDraft) : 0;
    },
    get sv() {
      const view = saveView(this.agentSave, { valid: this.agentCheck.valid, changes: this.agentChangeCount, writes: FILES, blocked: this.brokenWhy });
      if (view.blocked) return view;
      if (view.dirty && !this.agentCheck.valid && this.agentCheck.descriptionProblem) return { ...view, footer: `1 error · description is required` };
      return view;
    },
    get reviewFiles() {
      return this.sv.files;
    },
    get conflictFiles() {
      return this.sv.conflictFiles;
    },

    /** The editor's own errors: the MCP drawer, open over it, has its own, and the markup here must not show those. */
    get agentErrors() {
      const server = this.agentDraft ? agentFieldErrors(this.agentSave.refusal, this.agentDraft) : { name: "", description: "", aliases: "", entries: "", mcp: "", tools: "", projects: "", form: "" };
      return {
        name: this.agentCheck.nameProblem || server.name,
        description: this.agentCheck.descriptionProblem || server.description,
        aliases: this.aliasError || server.aliases,
        entries: server.entries,
        mcp: server.mcp,
        tools: server.tools,
        projects: server.projects,
        form: server.form,
      };
    },
    get fieldErrors() {
      return this.agentErrors;
    },
    get formInvalid() {
      return this.agentErrors.form !== "";
    },
    get refusalNext() {
      return this.agentSave.refusal?.next ?? "";
    },
    get refusalCode() {
      return this.agentSave.refusal?.code ?? "";
    },
    get agentNameInvalid() {
      return this.agentErrors.name !== "";
    },
    get agentDescInvalid() {
      return this.agentErrors.description !== "";
    },
    get agentAliasesInvalid() {
      return this.agentErrors.aliases !== "";
    },
    get entriesInvalid() {
      return this.agentErrors.entries !== "";
    },
    // The skills picker's own names: entriesInvalid is shared with the bundle editor, and under the MCP drawer it would answer for that.
    get agentEntriesInvalid() {
      return this.agentErrors.entries !== "";
    },
    get agentMcpInvalid() {
      return this.agentErrors.mcp !== "";
    },
    get agentProjectsInvalid() {
      return this.agentErrors.projects !== "";
    },
    get agentProjectsDescribedBy() {
      return this.agentProjectsInvalid ? "agent-projects-hint agent-projects-error" : "agent-projects-hint";
    },
    get agentNameDescribedBy() {
      return this.agentNameInvalid ? "agent-name-hint agent-name-error" : "agent-name-hint";
    },
    get agentDescDescribedBy() {
      return this.agentDescInvalid ? "agent-desc-hint agent-desc-error" : "agent-desc-hint";
    },
    get agentAliasesDescribedBy() {
      return this.agentAliasesInvalid ? "agent-aliases-hint agent-aliases-error" : "agent-aliases-hint";
    },
    get entriesDescribedBy() {
      return this.agentEntriesInvalid ? "agent-entries-hint agent-entries-error" : "agent-entries-hint";
    },
    get agentEntriesDescribedBy() {
      return this.agentEntriesInvalid ? "agent-entries-hint agent-entries-error" : "agent-entries-hint";
    },
    get agentMcpDescribedBy() {
      return this.agentMcpInvalid ? "agent-mcp-hint agent-mcp-error" : "agent-mcp-hint";
    },
    /** The banner above the form: the description rule, with a link to the field. */
    get noDescription() {
      return this.agentDraft !== null && this.agentDraft.description.trim() === "" && (this.agentTouched || this.agentDraft.previousName !== "");
    },

    // Skills.

    get skillsSummary() {
      return this.agentDraft && this.catalog ? resolvedSummary(this.catalog, this.agentDraft.entries) : { total: 0, text: "", missing: [], dropped: [] };
    },
    get skillsSummaryText() {
      return this.skillsSummary.text;
    },
    get skillsHasMissing() {
      return this.skillsSummary.missing.length > 0;
    },

    // Identity.

    /** Adds the alias typed; a bad one says why and stays in the box. */
    addAlias() {
      const alias = this.aliasQuery.trim().toLowerCase();
      if (alias === "") return;
      if (!isSlug(alias)) {
        this.aliasError = "An alias uses lowercase letters, digits and hyphens, and starts with a letter or digit.";
        return;
      }
      this.aliasError = "";
      this.aliasQuery = "";
      if (!this.agentDraft.aliases.includes(alias)) this.agentDraft.aliases.push(alias);
      this.agentChanged();
    },
    removeAlias(event) {
      const alias = event.currentTarget.dataset.alias;
      this.agentDraft.aliases = this.agentDraft.aliases.filter((a) => a !== alias);
      this.agentChanged();
    },
    aliasKey(event) {
      if (event.key === "Enter" || event.key === ",") {
        event.preventDefault();
        this.addAlias();
      } else if (event.key === "Backspace" && this.aliasQuery === "" && this.agentDraft.aliases.length > 0) {
        this.agentDraft.aliases = this.agentDraft.aliases.slice(0, -1);
        this.agentChanged();
      }
    },
    aliasInput() {
      this.aliasError = "";
    },
    get aliasChips() {
      return (this.agentDraft?.aliases ?? []).map((a) => ({ key: a, label: a, removeLabel: `Remove alias ${a}` }));
    },

    // MCP servers and tools.

    get mcpRowsChoice() {
      return this.catalog && this.agentDraft ? mcpChoices(this.catalog, this.agentDraft.mcpServers).map((r) => ({ ...r, id: `agent-mcp-${r.name}`, hasTransport: r.transport !== "", notInCatalog: r.missing })) : [];
    },
    get mcpSummary() {
      const n = this.agentDraft?.mcpServers.length ?? 0;
      return n ? `${n} of ${this.catalog?.mcpServers.length ?? 0} servers` : "No servers";
    },
    toggleServer(event) {
      const name = event.currentTarget.dataset.name;
      const on = this.agentDraft.mcpServers.includes(name);
      this.agentDraft.mcpServers = on ? this.agentDraft.mcpServers.filter((n) => n !== name) : [...this.agentDraft.mcpServers, name];
      this.agentChanged();
    },
    get toolsChoice() {
      return this.catalog && this.agentDraft ? toolChoices(this.catalog, this.agentDraft.allowedTools) : { rows: [], summary: "" };
    },
    get toolRowsChoice() {
      return this.toolsChoice.rows.map((r) => ({ ...r, id: `agent-tool-${r.name}` }));
    },
    get toolsSummary() {
      return this.toolsChoice.summary;
    },
    toggleTool(event) {
      const name = event.currentTarget.dataset.name;
      const on = this.agentDraft.allowedTools.includes(name);
      this.agentDraft.allowedTools = on ? this.agentDraft.allowedTools.filter((n) => n !== name) : [...this.agentDraft.allowedTools, name];
      this.agentChanged();
    },

    // Projects: where sessions can start as this agent (enabledIn.js).

    get projectsChoice() {
      return this.catalog && this.agentDraft ? projectChoices(this.catalog, this.agentDraft, this.projectList, this.projectModesKnown) : { rows: [], count: 0, total: 0, summary: "", tocCount: "" };
    },
    get projectRowsChoice() {
      return this.projectsChoice.rows;
    },
    get projectsSummary() {
      return this.projectsChoice.summary;
    },
    get noProjectRows() {
      return this.projectsChoice.rows.length === 0;
    },
    toggleProject(event) {
      this.agentDraft.projects = toggleProject(this.catalog, this.agentDraft, event.currentTarget.dataset.id);
      this.agentChanged();
    },

    /** The contents list, with what each section holds. */
    get toc() {
      const counts = this.agentDraft && this.catalog ? tocCounts(this.catalog, this.agentDraft, this.projectsChoice.tocCount) : { skills: "", mcp: "", tools: "", projects: "" };
      return [
        { id: "identity", label: "Identity", count: "" },
        { id: "skills", label: "Skills", count: counts.skills },
        { id: "mcp", label: "MCP servers", count: counts.mcp },
        { id: "tools", label: "Allowed tools", count: counts.tools },
        { id: "projects", label: "Projects", count: counts.projects },
      ].map((s) => ({ ...s, href: `#${s.id}`, current: s.id === this.tocOn ? "true" : null, hasCount: s.count !== "" }));
    },
    /** A click on the contents list: scroll there and say where we are. */
    gotoSection(event) {
      event.preventDefault();
      const id = event.currentTarget.dataset.section;
      this.tocOn = id;
      document.getElementById(id)?.scrollIntoView?.();
      history?.replaceState?.({}, "", `${location.pathname}${location.search ?? ""}#${id}`);
    },
    /** Called from x-init on the page: follows the section in view. @param {HTMLElement} el */
    watchSections(el) {
      if (typeof IntersectionObserver === "undefined") return;
      const io = new IntersectionObserver((entries) => {
        for (const e of entries) if (e.isIntersecting) this.tocOn = e.target.id;
      }, { rootMargin: "-20% 0px -70% 0px" });
      for (const s of el.querySelectorAll("section[id]")) io.observe(s);
    },
    focusDescription(event) {
      event.preventDefault();
      document.getElementById("agent-desc")?.focus();
    },

    /** The form changed: the pipeline schedules the preview. */
    agentChanged() {
      this.agentTouched = true;
      saver.edited({ dirty: this.agentChangeCount > 0, valid: this.agentCheck.valid });
    },

    /** Moves the keyboard to the first field the form or the server found wrong. */
    focusFirstInvalid() {
      this.$nextTick?.(() => {
        document.querySelector("[data-agent-editor] [aria-invalid='true']")?.focus();
        // Arriving by focus must not open the list over the chips the error is about.
        this.skillPickerOpen = false;
      });
    },

    async saveEditor() {
      if (!this.sv.canSave) return;
      await this.finishAgent(await saver.save());
    },
    async overwrite() {
      await this.finishAgent(await saver.save({ force: true }));
    },
    /** A saved agent: read the catalog again, show the toast, and land on the agent. */
    async finishAgent({ ok, result }) {
      if (!ok || !result) {
        this.focusFirstInvalid();
        return;
      }
      const name = this.agentDraft.name;
      this.showToast(savedToast({ id: result.change.id, summary: result.change.summary }));
      await this.load();
      this.closeEditor();
      this.navigateTo(`/agents/${enc(name)}`);
    },
    async reloadEditor() {
      const previous = this.agentDraft.previousName;
      this.clearDisk("agent", previous);
      await this.load();
      const draft = this.catalog ? agentDraft(this.catalog, previous) : null;
      if (!draft) {
        this.notice = "That agent no longer exists";
        this.closeEditor();
        this.navigateTo("/agents");
        return;
      }
      this.agentDraft = draft;
      this.agentTouched = false;
      this.reviewOpen = false;
      saver.reload(versionsOf(this.catalog));
    },
    /** The catalog was read again: settle this editor against what is on disk now (liveCatalog.js). */
    catalogMoved(ctx) {
      if (this.agentDraft !== null) settleEditor(saver, "agent", this.agentDraft.previousName, ctx);
    },
    cancelEditor(event) {
      event.preventDefault();
      const previous = this.agentDraft?.previousName ?? "";
      this.guarded(() => this.navigateTo(previous ? `/agents/${enc(previous)}` : "/agents"));
    },
  };

  return Object.defineProperties({}, { ...Object.getOwnPropertyDescriptors(picker), ...Object.getOwnPropertyDescriptors(editor) });
}
