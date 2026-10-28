// The MCP server form, at /agents/mcp/new and /agents/mcp/<name>/edit, and as a
// side drawer over the agent editor: the name, two cards (Local command,
// Remote URL), the command with argument rows that split a pasted command
// line, env and header rows with From env / Plain text, the token warning, the
// set / not set indicator per ${VAR}, Test connection, Who gets it, the rename
// banner, and beside them What will be written, the server's dry run as the
// person types. The save pipeline (Review change, conflict, toast, leave
// guard) is the one the other editors share.
//
// Opened from another page it has a way back: ?return=<path>. A save goes there
// with ?added=<server> before the #fragment, Cancel goes there without it. In
// the agent editor it is a drawer instead: it saves the server unattached,
// ticks it in the editor, and the half-done agent edit stays in memory.
// Mixed into the Agents page, which provides the catalog, the route
// (tab, item, mode), `load()`, `navigateTo()` and the toast.
//
// A secret never leaves its field: a token typed as plain text blocks the
// preview, the test and the save until the person settles it; the server's
// answers are hidden of any value the form holds before the page shows them.
// Every member is prefixed mf, except the ones the save pipeline's markup
// binds to by name (see editors.js).

import { describeError } from "../../../../shared/presentation/errors.js";
import {
  addRow, checkMcpDraft, detectToken, hideSecrets, mcpChanges, mcpDraft, mcpFieldErrors, mcpPreviewModel, mcpRefs, mcpSave,
  probeView, referenceName, removeRow, rename, setMode, splitCommandLine, toReference, useReference, whoGetsIt,
} from "../../domain/mcpForm.js";
import { openSave, saveView, savedToast } from "../../domain/savePipeline.js";
import { returnHref, safeReturn } from "../../domain/skillForm.js";
import { trapTab } from "./focusTrap.js";
import { settleEditor } from "./liveCatalog.js";
import { saveController } from "./saveController.js";

const enc = encodeURIComponent;
const plural = (n, one, many = `${one}s`) => `${n} ${n === 1 ? one : many}`;
const byName = (a, b) => a.name.localeCompare(b.name);
const FILES = ["agents.json", "mcp-servers.json"];
/** How long typing pauses before the variables are looked up. */
const ENV_DELAY_MS = 300;
/** The server stops a test at this many seconds. */
const PROBE_LIMIT_S = 20;
const KIND = { args: "arg", env: "env", headers: "header" };
const article = (kind) => `${/^[AEIOU]/.test(kind) ? "an" : "a"} ${kind}`;

/** The versions of the files this form writes, as the catalog read them. */
const versionsOf = (catalog) => Object.fromEntries(FILES.filter((f) => f in catalog.versions).map((f) => [f, catalog.versions[f]]));

/**
 * @param {{
 *   gateway: Pick<import("../../domain/ports.js").CatalogGateway, "saveMcpServer" | "testMcpServer" | "checkEnvVars" | "listProjects">,
 *   location: { pathname: string, search?: string },
 *   timers?: { setTimeout: (fn: () => void, ms: number) => any, clearTimeout: (id: any) => void },
 * }} deps
 */
export function mcpEditor({ gateway, location, timers = globalThis }) {
  /** @type {any} */
  let page;
  /** @type {ReturnType<typeof saveController>} */
  let saver;
  let envTimer = null;
  let tickTimer = null;
  // The diff models are worked out once per preview, not on every read.
  const models = new WeakMap();

  const stopTimers = () => {
    if (envTimer !== null) timers.clearTimeout(envTimer);
    if (tickTimer !== null) timers.clearTimeout(tickTimer);
    envTimer = tickTimer = null;
  };

  const editor = {
    mfOpen: false,
    /** The form is the agent editor's drawer, not the page. */
    mfDrawer: false,
    /** @type {import("../../domain/mcpForm.js").McpDraft | null} */
    mfDraft: null,
    mfSave: openSave({}),
    /** The last preview the server answered, kept while the next is on its way. */
    mfShown: null,
    mfTouched: false,
    mfMissing: false,
    /** The way back to the page the person came from. */
    mfBack: "",
    mfNote: "",
    /** @type {{ id: string, name: string }[]} */
    mfProjects: [],
    /** Whether each variable is set on the harness server: true, false, or null when it could not be asked. */
    mfEnvKnown: {},
    mfEnvChecking: false,
    /** The variable a row's token moved to, by row id, until the row changes. */
    mfUsed: {},
    /** @type {import("../../domain/ports.js").Probe | null} */
    mfProbe: null,
    mfProbeRunning: false,
    mfProbeSeconds: 0,
    /** How many edits the form had when the test started; another count means the answer is old. */
    mfProbeAt: 0,
    mfEdits: 0,
    mfSeq: 0,

    setupEditor() {
      page = this;
      saver = saveController({
        run: async (options) => {
          const draft = page.mfDraft;
          const answer = await gateway.saveMcpServer(mcpSave(draft, { unattached: page.mfDrawer }), options);
          return options.dryRun ? { ...answer, change: hideSecrets(answer.change, draft) } : answer;
        },
        onState: (state) => {
          page.mfSave = state;
          if (state.preview) page.mfShown = state.preview;
        },
        blocked: () => page.brokenWhy !== "",
        timers,
      });
    },

    /** Opens, or closes, the form the route names. Called after every move of the route and the first read. */
    syncEditor() {
      const wanted = this.catalog !== null && this.tab === "mcp" && this.mode !== "";
      if (!wanted) {
        if (this.mfOpen) this.mfClose();
        return;
      }
      this.mfBegin({ drawer: false });
    },

    /** Starts the form: empty, a copy, or the server read from the catalog. */
    mfBegin({ drawer }) {
      const seq = ++this.mfSeq;
      const params = new URLSearchParams(drawer ? "" : location.search ?? "");
      stopTimers();
      this.mfOpen = true;
      this.mfDrawer = drawer;
      this.mfDraft = null;
      this.mfShown = null;
      this.mfTouched = false;
      this.mfMissing = false;
      this.mfBack = drawer ? "" : safeReturn(params.get("return"));
      this.mfNote = "";
      this.mfUsed = {};
      this.mfProbe = null;
      this.mfProbeRunning = false;
      this.mfProbeAt = 0;
      this.mfEdits = 0;
      this.mfEnvChecking = false;
      this.reviewOpen = false;
      if (!drawer) {
        this.departing = false;
        this.leaving = null;
      }
      saver.open({});

      const edit = this.mode === "edit" && !drawer;
      const find = (name) => this.catalog.mcpServers.find((s) => s.name === name);
      if (edit && !find(this.item)) {
        this.mfMissing = true;
        return;
      }
      const copy = !edit ? find(params.get("from") ?? "") : undefined;
      const draft = edit ? mcpDraft(this.catalog, { previous: find(this.item) }) : mcpDraft(this.catalog, { from: copy, name: params.get("name") ?? "" });
      this.mfDraft = draft;
      saver.open(versionsOf(this.catalog));
      // What the catalog already says about its variables is known without asking.
      const known = {};
      for (const s of this.catalog.mcpServers) for (const r of s.envRefs) known[r.name] = r.set;
      this.mfEnvKnown = known;
      if (mcpChanges(draft) > 0 && !edit) this.mfChanged();
      else this.mfScheduleEnv();
      this.mfLoadProjects(seq);
      if (drawer) this.$nextTick?.(() => document.getElementById("mf-name")?.focus());
    },

    /** The projects Who gets it can list; the catalog's own lists stand in when they cannot be read. */
    async mfLoadProjects(seq) {
      let list;
      try {
        list = await gateway.listProjects();
      } catch {
        list = [...new Map(this.catalog.mcpServers.flatMap((s) => s.projects).map((p) => [p.id, p])).values()];
      }
      if (seq === this.mfSeq) this.mfProjects = list;
    },

    closeEditor() {
      if (this.mfOpen) this.mfClose();
    },
    mfClose() {
      this.mfSeq++;
      stopTimers();
      saver.open({});
      this.mfOpen = false;
      this.mfDrawer = false;
      this.mfDraft = null;
      this.mfShown = null;
      this.mfMissing = false;
      this.mfProbe = null;
      this.mfProbeRunning = false;
      this.reviewOpen = false;
      this.leaving = null;
    },

    // The page.

    get mfReady() {
      return this.mfDraft !== null;
    },
    get mfIsEdit() {
      return this.mfDraft ? this.mfDraft.previousName !== "" : this.mode === "edit" && !this.mfDrawer;
    },
    get mfTitle() {
      return this.mfIsEdit ? `Edit ${this.mfDraft?.previousName || this.item}` : "New MCP server";
    },
    get mfSubtitle() {
      if (this.mfDrawer) return "Adds an entry to the catalog's mcp-servers.json on its own. It is ticked in the agent when you come back; the agent gets it when you save the agent.";
      return "Adds an entry to the catalog's mcp-servers.json, defined once under a unique name. Pick the agents and projects that get it, or none yet. Secrets stay as ${VARIABLE} references; the harness stores no secret, its values come from the harness server's environment.";
    },
    get mfPathChip() {
      return this.mfIsEdit ? `mcp-servers.json › mcpServers.${this.mfDraft?.previousName || this.item}` : "will be added to mcp-servers.json › mcpServers";
    },
    get mfBackLabel() {
      return this.mfBack ? "← Back to the agent" : "← MCP servers";
    },
    get mfBackHref() {
      return this.mfBack || (this.mfIsEdit ? `/agents/mcp/${enc(this.mfDraft?.previousName || this.item)}` : "/agents/mcp");
    },
    get mfMode() {
      return this.mfDrawer ? "drawer" : "page";
    },
    /** The page's scroll region is the form's; a drawer scrolls itself. */
    get mfScrollAttr() {
      return this.mfDrawer ? null : "";
    },
    get mfRole() {
      return this.mfDrawer ? "dialog" : null;
    },
    get mfModal() {
      return this.mfDrawer ? "true" : null;
    },
    /** Under the drawer the agent editor is inert: the keyboard stays in the drawer. */
    get mfAgentInert() {
      return this.mfOpen && this.mfDrawer ? true : null;
    },
    get mfNotDrawer() {
      return !this.mfDrawer;
    },
    get mfAttachable() {
      return !this.mfDrawer;
    },
    get mfLoadingShown() {
      return this.mfOpen && !this.mfReady && !this.mfMissing;
    },
    get mfMissingShown() {
      return this.mfMissing;
    },
    get editorHref() {
      return `${location.pathname}${location.search ?? ""}`;
    },
    get saveButtonLabel() {
      if (this.sv.saving) return "Saving…";
      return this.mfIsEdit ? "Save server" : "Add server";
    },
    get mfHasNote() {
      return this.mfNote !== "";
    },

    // Checks.

    get mfCheck() {
      return this.mfDraft && this.catalog
        ? checkMcpDraft(this.mfDraft, this.catalog, { touched: this.mfTouched || this.mfDraft.previousName !== "" })
        : checkMcpDraft({ previousName: "", name: "", transport: "stdio", command: "", args: [], env: [], url: "", headers: [], agents: [], projects: [], base: {} }, { mcpServers: [] });
    },
    get mfChangeCount() {
      return this.mfDraft ? mcpChanges(this.mfDraft) : 0;
    },
    /** The name another server already has: the form knows at once, the server's NAME_TAKEN covers a catalog that moved. */
    get mfTaken() {
      if (this.mfCheck.taken) return this.mfCheck.taken;
      const refusal = this.mfSave.refusal;
      if (refusal?.code !== "NAME_TAKEN" || !this.mfDraft) return null;
      return { name: this.mfDraft.name, href: `/agents/mcp/${enc(this.mfDraft.name)}`, suggestion: refusal.suggestion };
    },
    get mfWrites() {
      const d = this.mfDraft;
      if (!d) return ["mcp-servers.json"];
      const base = /** @type {any} */ (d.base);
      const sym = (a, b) => a.some((x) => !b.includes(x)) || b.some((x) => !a.includes(x));
      const agents = sym(d.agents, base.agents ?? []) || (d.previousName !== "" && d.name !== d.previousName && (this.catalog?.mcpServers.find((s) => s.name === d.previousName)?.agents.length ?? 0) > 0);
      const projects = sym(d.projects, base.projects ?? []);
      return ["mcp-servers.json", ...(agents ? ["agents.json"] : []), ...(projects ? ["project lists"] : [])];
    },
    get sv() {
      const view = saveView(this.mfSave, { valid: this.mfCheck.valid, changes: this.mfChangeCount, writes: this.mfWrites, blocked: this.brokenWhy });
      if (view.blocked) return view;
      const verb = this.mfIsEdit ? "save" : "add";
      if (view.conflict) return { ...view, footer: "Saving is off until you choose Reload or Overwrite." };
      if (!view.dirty) return this.mfIsEdit ? view : { ...view, footer: "Nothing is written until you add the server." };
      const check = this.mfCheck;
      if (check.blocked) return { ...view, footer: `Settle the token warning to ${verb} the server.` };
      if (!check.valid) {
        const d = this.mfDraft;
        const target = d?.transport === "stdio" ? (d.command.trim() === "" || check.commandProblem !== "") : ((d?.url.trim() ?? "") === "" || check.urlProblem !== "");
        const bad = [d?.name === "" || check.nameProblem !== "", target, check.argsProblem !== "" || check.envProblem !== "" || check.headersProblem !== ""].filter(Boolean).length;
        return { ...view, footer: `Fix ${plural(Math.max(bad, 1), "field")} to ${verb} the server.` };
      }
      const tail = this.mfDrawer ? "Saved on its own; the agent gets it when you save the agent." : this.mfWho.short;
      return { ...view, footer: `${view.footer}. ${tail}` };
    },
    get reviewFiles() {
      return this.sv.files;
    },
    get conflictFiles() {
      return this.sv.conflictFiles;
    },

    get fieldErrors() {
      const server = mcpFieldErrors(this.mfSave.refusal);
      const check = this.mfCheck;
      return {
        name: this.mfTaken ? `A server named ${this.mfTaken.name} already exists.` : check.nameProblem || server.name,
        command: check.commandProblem || server.command,
        url: check.urlProblem || server.url,
        args: check.argsProblem || server.args,
        env: check.envProblem || server.env,
        headers: check.headersProblem || server.headers,
        form: server.form,
      };
    },
    get formInvalid() {
      return this.fieldErrors.form !== "";
    },
    get refusalNext() {
      return this.mfSave.refusal?.next ?? "";
    },
    get refusalCode() {
      return this.mfSave.refusal?.code ?? "";
    },
    get mfNameInvalid() {
      return this.fieldErrors.name !== "";
    },
    get mfCommandInvalid() {
      return this.fieldErrors.command !== "";
    },
    get mfUrlInvalid() {
      return this.fieldErrors.url !== "";
    },
    get mfArgsInvalid() {
      return this.fieldErrors.args !== "";
    },
    get mfEnvInvalid() {
      return this.fieldErrors.env !== "";
    },
    get mfHeadersInvalid() {
      return this.fieldErrors.headers !== "";
    },
    get mfNameDescribedBy() {
      return this.mfNameInvalid ? "mf-name-hint mf-name-error" : "mf-name-hint";
    },
    get mfCommandDescribedBy() {
      return this.mfCommandInvalid ? "mf-command-hint mf-command-error" : "mf-command-hint";
    },
    get mfUrlDescribedBy() {
      return this.mfUrlInvalid ? "mf-url-hint mf-url-error" : "mf-url-hint";
    },
    get mfArgsDescribedBy() {
      return this.mfArgsInvalid ? "mf-args-hint mf-args-error" : "mf-args-hint";
    },
    get mfEnvDescribedBy() {
      return this.mfEnvInvalid ? "mf-env-error" : null;
    },
    get mfHeadersDescribedBy() {
      return this.mfHeadersInvalid ? "mf-headers-error" : null;
    },
    get mfHasSuggestion() {
      return this.mfTaken !== null && this.mfTaken.suggestion !== "";
    },
    get mfTakenHref() {
      return this.mfTaken?.href ?? "";
    },
    get mfSuggestion() {
      return this.mfTaken?.suggestion ?? "";
    },
    get mfTakenShown() {
      return this.mfTaken !== null;
    },

    // The rename banner.

    get mfRename() {
      return this.mfDraft && this.catalog ? rename(this.catalog, this.mfDraft) : { show: false, text: "" };
    },
    get mfRenameShown() {
      return this.mfRename.show;
    },
    get mfRenameText() {
      return this.mfRename.text;
    },

    // Transport.

    get mfLocal() {
      return this.mfDraft?.transport === "stdio";
    },
    get mfRemote() {
      return this.mfDraft !== null && this.mfDraft.transport !== "stdio";
    },
    get mfLocalPressed() {
      return String(this.mfLocal);
    },
    get mfRemotePressed() {
      return String(this.mfRemote);
    },
    mfPickTransport(event) {
      const wanted = event.currentTarget.dataset.transport;
      const d = this.mfDraft;
      if (wanted === "stdio") d.transport = "stdio";
      else if (d.transport === "stdio") d.transport = "http";
      this.mfChanged();
    },

    // Command and arguments.

    /** A whole command line was pasted into Command: the first word stays, the rest become one argument per row. */
    mfCommandPaste(event) {
      const text = event.clipboardData?.getData("text") ?? "";
      if (!/\s/.test(text.trim())) return;
      event.preventDefault();
      this.mfSplitCommand(text);
      this.mfChanged();
    },
    /** Command lost focus with a line in it (typed, autofilled): the same split. */
    mfCommandInput() {
      if (/\s/.test(this.mfDraft.command.trim())) this.mfSplitCommand(this.mfDraft.command);
      this.mfChanged();
    },
    mfSplitCommand(text) {
      const parts = splitCommandLine(text);
      if (parts.length === 0) return;
      const d = this.mfDraft;
      d.command = parts[0];
      d.args = [];
      for (const value of parts.slice(1)) {
        addRow(d, "args");
        d.args[d.args.length - 1].value = value;
      }
      this.mfNote = parts.length > 1 ? `Split into a command and ${plural(parts.length - 1, "argument")}.` : "";
    },

    // Rows.

    /** One row as the markup binds it. */
    mfRowView(section, row, index) {
      const check = this.mfCheck;
      const found = check.secrets.find((s) => s.section === section && s.id === row.id);
      const secret = found
        ? {
            state: found.state,
            isWarn: found.state === "warn",
            isKept: found.state === "kept",
            title: found.state === "warn" ? `This looks like ${article(found.kind)}.` : "Kept as plain text.",
            detail: found.state === "warn"
              ? "As plain text it would be written into mcp-servers.json. The harness stores no secrets, so it cannot keep the value for you."
              : "The key is written as it is into mcp-servers.json in the data directory.",
            tone: "attention",
            useLabel: "Use ${" + found.variable + "} instead",
            useVariant: found.state === "warn" ? "primary" : "ghost",
          }
        : null;
      const used = this.mfUsed[row.id];
      const label = section === "args" ? `Argument ${index + 1}` : section === "env" ? "Variable" : "Header";
      return {
        id: row.id,
        key: row.key ?? "",
        value: row.value,
        isEnv: row.mode === "env",
        modeValue: row.mode ?? "text",
        inputType: secret ? "password" : "text",
        invalid: secret?.isWarn ? "true" : null,
        secret,
        hasSecret: secret !== null,
        noSecret: secret === null,
        used: used ? `The pasted key is gone from this form. The harness does not store it. Set ${used} to that key in the harness server's environment, then restart the server if it is not read live.` : "",
        hasUsed: Boolean(used),
        noteId: `mf-note-${row.id}`,
        describedBy: secret || used ? `mf-note-${row.id}` : null,
        keyAria: `${label} name`,
        valueAria: section === "args" ? label : `${label} value`,
        modeAria: `${label} value kind`,
        removeAria: `Remove ${section === "args" ? label.toLowerCase() : `${label.toLowerCase()} ${row.key || index + 1}`}`,
      };
    },
    get mfArgRows() {
      return (this.mfDraft?.args ?? []).map((r, i) => this.mfRowView("args", r, i));
    },
    get mfEnvRows() {
      return (this.mfDraft?.env ?? []).map((r, i) => this.mfRowView("env", r, i));
    },
    get mfHeaderRows() {
      return (this.mfDraft?.headers ?? []).map((r, i) => this.mfRowView("headers", r, i));
    },
    mfRowOf(event) {
      const { section, id } = event.currentTarget.dataset;
      return { section, row: this.mfDraft[section].find((r) => String(r.id) === id) };
    },
    mfAddRow(event) {
      const section = event.currentTarget.dataset.section;
      addRow(this.mfDraft, section);
      const id = this.mfDraft[section][this.mfDraft[section].length - 1].id;
      this.mfChanged();
      this.$nextTick?.(() => document.querySelector(`[data-mf-row="${id}"] input`)?.focus());
    },
    mfRemoveRow(event) {
      const { section, row } = this.mfRowOf(event);
      if (!row) return;
      removeRow(this.mfDraft, section, row.id);
      this.mfChanged();
    },
    /** A key or value is typed: a token flips its row to Plain text, and an edit takes back any choice made about it. */
    mfRowInput(event) {
      const { section, row } = this.mfRowOf(event);
      if (!row) return;
      const field = event.currentTarget.dataset.field;
      const text = event.currentTarget.value;
      if (field === "key") {
        row.key = text;
      } else {
        row.value = text;
        row.kept = false;
        if (row.mode === "env" && detectToken(text)) row.mode = "text";
        this.mfUsed = Object.fromEntries(Object.entries(this.mfUsed).filter(([id]) => id !== String(row.id)));
      }
      this.mfChanged();
    },
    /** A value lost focus: under From env a bare variable name becomes its ${reference}. */
    mfRowChange(event) {
      const { row } = this.mfRowOf(event);
      if (!row || event.currentTarget.dataset.field !== "value" || row.mode !== "env") return;
      const wrapped = toReference(row.value);
      if (wrapped === row.value) return;
      row.value = wrapped;
      this.mfChanged();
    },
    mfRowMode(event) {
      const { section, row } = this.mfRowOf(event);
      if (!row) return;
      setMode(this.mfDraft, section, row.id, event.currentTarget.value === "env" ? "env" : "text");
      this.mfChanged();
    },
    /** Use ${VAR} instead: the token is replaced by a reference, and the form says where to set it. */
    mfUseVariable(event) {
      const { section, row } = this.mfRowOf(event);
      if (!row) return;
      const name = referenceName({ kind: KIND[section], key: row.key ?? "", server: this.mfDraft.name });
      row.value = useReference(row.value, name);
      if ("mode" in row) row.mode = "env";
      row.kept = false;
      this.mfUsed = { ...this.mfUsed, [row.id]: name };
      this.mfChanged();
    },
    /** Keep as text: the person chose to write the value into the catalog. */
    mfKeepAsText(event) {
      const { row } = this.mfRowOf(event);
      if (!row) return;
      row.kept = true;
      this.mfChanged();
    },

    // ${VAR}: set or not set on the harness server.

    get mfEnvRefs() {
      if (!this.mfDraft) return [];
      return mcpRefs(this.mfDraft).map((name) => {
        const known = this.mfEnvKnown[name];
        const state = known === true ? "set" : known === false ? "not set" : known === null ? "not checked" : "checking";
        return { name, label: "${" + name + "}", state, tone: known === true ? "success" : known === false ? "attention" : "" };
      });
    },
    get mfHasEnvRefs() {
      return this.mfEnvRefs.length > 0;
    },
    get mfEnvUnset() {
      return this.mfEnvRefs.some((r) => r.state === "not set");
    },
    get mfEnvUnsetText() {
      const unset = this.mfEnvRefs.filter((r) => r.state === "not set").map((r) => r.label);
      return `${unset.join(", ")} ${unset.length === 1 ? "is" : "are"} not set in the harness server's environment. Saving works; the server fails to start until it is set.`;
    },
    get mfCheckLabel() {
      return this.mfEnvChecking ? "Checking…" : "Check again";
    },
    /** After a pause in typing, asks about the variables the server has not been asked about. */
    mfScheduleEnv() {
      if (envTimer !== null) timers.clearTimeout(envTimer);
      envTimer = timers.setTimeout(() => {
        envTimer = null;
        this.mfAskEnv(false);
      }, ENV_DELAY_MS);
    },
    async mfAskEnv(all) {
      if (!this.mfDraft) return;
      const names = mcpRefs(this.mfDraft).filter((n) => all || !(n in this.mfEnvKnown));
      if (names.length === 0) return;
      const seq = this.mfSeq;
      try {
        const answer = await gateway.checkEnvVars(names);
        if (seq === this.mfSeq) this.mfEnvKnown = { ...this.mfEnvKnown, ...Object.fromEntries(names.map((n) => [n, answer[n] === true])) };
      } catch {
        if (seq === this.mfSeq) this.mfEnvKnown = { ...this.mfEnvKnown, ...Object.fromEntries(names.map((n) => [n, null])) };
      }
    },
    async mfCheckAgain() {
      this.mfEnvChecking = true;
      try {
        await this.mfAskEnv(true);
      } finally {
        this.mfEnvChecking = false;
      }
    },

    // Test connection.

    get mfTestable() {
      return this.mfCheck.testable && !this.mfProbeRunning;
    },
    get mfTestDisabled() {
      return !this.mfTestable;
    },
    get mfTestLabel() {
      return this.mfProbeRunning ? "Testing…" : "Test connection";
    },
    get mfTestBusy() {
      return String(this.mfProbeRunning);
    },
    get mfProbeWait() {
      return `Connecting… ${this.mfProbeSeconds} s. The test stops after ${PROBE_LIMIT_S} s.`;
    },
    get mfProbeView() {
      return probeView(this.mfProbe);
    },
    get mfProbeShown() {
      return this.mfProbe !== null;
    },
    get mfProbeStale() {
      return this.mfProbe !== null && this.mfProbeAt !== this.mfEdits ? "true" : null;
    },
    get mfProbeMore() {
      return this.mfProbeView.more > 0;
    },
    get mfProbeMoreText() {
      return `+${this.mfProbeView.more} more`;
    },
    get mfProbeHasHint() {
      return this.mfProbeView.hint !== "";
    },
    get mfProbeIsOk() {
      return this.mfProbeView.phase === "ok";
    },
    get mfProbeIsError() {
      return this.mfProbeView.phase === "error";
    },
    get mfProbeMark() {
      return this.mfProbeIsOk ? "✓" : "✕";
    },
    /** Starts the server once on the harness server and lists its tools; it stores nothing. The wait shows. */
    async mfTest() {
      if (!this.mfTestable) return;
      const seq = this.mfSeq;
      const server = mcpSave(this.mfDraft).server;
      this.mfProbe = null;
      this.mfProbeRunning = true;
      this.mfProbeSeconds = 0;
      this.mfProbeAt = this.mfEdits;
      const tick = () => {
        tickTimer = timers.setTimeout(() => {
          this.mfProbeSeconds++;
          tick();
        }, 1000);
      };
      tick();
      let probe;
      try {
        probe = await gateway.testMcpServer(server);
      } catch (err) {
        probe = { status: "error", tools: [], error: describeError(err).message, hint: "" };
      }
      if (seq !== this.mfSeq) return;
      if (tickTimer !== null) timers.clearTimeout(tickTimer);
      tickTimer = null;
      this.mfProbe = probe;
      this.mfProbeRunning = false;
    },

    // Who gets it.

    get mfAgentRows() {
      if (!this.catalog || !this.mfDraft) return [];
      return [...this.catalog.agents].sort(byName).map((a) => ({
        name: a.name,
        checked: this.mfDraft.agents.includes(a.name),
        hint: a.mcpServers.length === 0 ? "no servers now" : `${plural(a.mcpServers.length, "server")} now`,
      }));
    },
    get mfProjectRows() {
      if (!this.mfDraft) return [];
      return [...this.mfProjects].sort(byName).map((p) => ({ id: p.id, name: p.name, checked: this.mfDraft.projects.includes(p.id), hint: "every session in the project gets it" }));
    },
    get mfNoAgents() {
      return (this.catalog?.agents.length ?? 0) === 0;
    },
    get mfNoProjects() {
      return this.mfProjects.length === 0;
    },
    get mfWho() {
      return this.mfDraft ? whoGetsIt(this.mfDraft) : whoGetsIt({ agents: [], projects: [] });
    },
    get mfWhoText() {
      return this.mfWho.text;
    },
    get mfWhoTone() {
      return this.mfWho.unused ? "attention" : "";
    },
    mfToggleAgent(event) {
      const name = event.currentTarget.dataset.name;
      const d = this.mfDraft;
      d.agents = d.agents.includes(name) ? d.agents.filter((a) => a !== name) : [...d.agents, name];
      this.mfChanged();
    },
    mfToggleProject(event) {
      const id = event.currentTarget.dataset.id;
      const d = this.mfDraft;
      d.projects = d.projects.includes(id) ? d.projects.filter((p) => p !== id) : [...d.projects, id];
      this.mfChanged();
    },

    // What will be written.

    get mfPreviewFiles() {
      const p = this.mfShown;
      if (!p) return [];
      if (!models.has(p)) models.set(p, mcpPreviewModel(p));
      return models.get(p);
    },
    get mfHasPreview() {
      return this.mfShown !== null;
    },
    get mfPreviewStale() {
      return this.mfShown !== null && this.mfSave.preview === null && this.mfSave.phase !== "saved" ? "true" : null;
    },
    get mfPreviewCount() {
      const p = this.mfShown;
      if (!p) return "";
      const files = p.writes.length;
      const lists = p.projects.length;
      return [files ? plural(files, "file") : "", lists ? plural(lists, "project list") : ""].filter(Boolean).join(" · ");
    },
    get mfPreviewHint() {
      if (this.mfShown !== null) return "";
      if (this.mfSave.phase === "previewing") return "Asking the server what it would write…";
      if (this.mfSave.refusal && !this.formInvalid) return "No preview until the error above is fixed.";
      if (this.mfSave.refusal) return "No preview: the server refused. Its answer is above.";
      if (this.mfCheck.blocked) return "No preview while a token waits for your choice: it is sent nowhere until you settle it.";
      if (!this.mfCheck.valid) return "Give the server a name and a command or URL, and the files it will write show here.";
      return this.mfIsEdit ? "No changes to write." : "Type a name and a command or URL to see the files.";
    },
    get mfPreviewBusy() {
      return this.mfSave.phase === "previewing" || (this.mfSave.phase === "dirty" && this.mfSave.preview === null && this.mfCheck.valid) ? "true" : "false";
    },

    // Changing the form.

    /** The name loses nothing typed; the pipeline schedules the preview. */
    mfNameInput() {
      this.mfChanged();
    },
    mfUseSuggestion() {
      this.mfDraft.name = this.mfSuggestion;
      this.mfChanged();
    },
    mfChanged() {
      this.mfTouched = true;
      this.mfEdits++;
      saver.edited({ dirty: this.mfChangeCount > 0, valid: this.mfCheck.valid });
      this.mfScheduleEnv();
    },
    /** Moves the keyboard to the first field the form or the server found wrong. */
    mfFocusFirstInvalid() {
      this.$nextTick?.(() => document.querySelector("[data-mcp-form] [aria-invalid='true']")?.focus());
    },

    // Opening it as a drawer, saving, and the way out.

    /** The agent editor's New MCP server…: the form opens over the edit, which stays as it is. */
    mfOpenDrawer() {
      if (!this.agentDraft) return;
      this.mfBegin({ drawer: true });
    },

    async saveEditor() {
      if (!this.sv.canSave) return;
      await this.mfFinish(await saver.save());
    },
    async overwrite() {
      await this.mfFinish(await saver.save({ force: true }));
    },
    /** A saved server: read the catalog again, show the toast, and go back to the page it was made for, or to the server. */
    async mfFinish({ ok, result }) {
      if (!ok || !result) {
        this.mfFocusFirstInvalid();
        return;
      }
      const name = this.mfDraft.name.trim();
      const drawer = this.mfDrawer;
      const back = this.mfBack;
      this.showToast(savedToast({ id: result.change.id, summary: result.change.summary }));
      await this.load();
      if (drawer) {
        // The agent gets the server when the person saves the agent: here it is only ticked.
        this.mfClose();
        const d = this.agentDraft;
        if (d && !d.mcpServers.includes(name)) d.mcpServers.push(name);
        this.agentChanged?.();
        this.$nextTick?.(() => document.querySelector("[data-new-mcp]")?.focus());
        return;
      }
      this.closeEditor();
      this.navigateTo(back ? returnHref(back, name) : `/agents/mcp/${enc(name)}`);
    },
    /** The conflict banner's Reload: drop my edits and read the server again. */
    async reloadEditor() {
      const drawer = this.mfDrawer;
      this.clearDisk("mcp", this.mfDraft?.previousName ?? "");
      await this.load();
      this.mfBegin({ drawer });
    },
    /** The catalog was read again: settle this editor against what is on disk now (liveCatalog.js). */
    catalogMoved(ctx) {
      if (this.mfOpen && this.mfDraft) settleEditor(saver, "mcp", this.mfDraft.previousName, ctx);
    },
    cancelEditor(event) {
      event.preventDefault();
      if (this.mfDrawer) {
        // Only the drawer goes: the agent editor under it keeps its edit.
        const close = () => {
          this.mfClose();
          this.$nextTick?.(() => document.querySelector("[data-new-mcp]")?.focus());
        };
        if (this.leaveGuarded) this.leaving = { go: close, inPlace: true };
        else close();
        return;
      }
      const go = this.mfBack ? returnHref(this.mfBack, "") : this.mfBackHref;
      this.guarded(() => this.navigateTo(go));
    },
    /** The back link: the same as Cancel. */
    mfBackClick(event) {
      this.cancelEditor(event);
    },
    /** The drawer is modal: Tab wraps inside it. The full page has no use for it. @param {KeyboardEvent} event */
    mfTrapTab(event) {
      if (this.mfDrawer) trapTab(event);
    },
    /** Escape closes the drawer as Cancel does; the full page has no use for it. */
    mfEscape(event) {
      if (!this.mfOpen || !this.mfDrawer) return;
      this.cancelEditor(event);
    },
  };

  return editor;
}
