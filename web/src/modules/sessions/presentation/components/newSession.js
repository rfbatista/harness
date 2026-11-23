// The new-session form: starts an interactive session on the page's task,
// in a terminal on the server; the page then shows that terminal.
// The server renders the choices (agents, repositories); this holds the
// draft and submits it. The page passes its project and task in and listens
// for session-created / new-session-cancelled.
//
//   <form x-data="sessionsNewSession(projectId, ticketId)"
//         data-default-repository="r1" x-on:submit.prevent="submit">
//
// On the project's sessions page there is no task and the form is locked to
// product mode, the one mode that runs without a task:
//   <form x-data="sessionsNewSession(projectId, ticketId)" data-locked-mode="product" …>

import { describeError } from "../../../../shared/presentation/errors.js";
import { Codes } from "../../../../shared/domain/errors.js";
import { INITIAL_TERMINAL_SIZE, noTaskProblem } from "../../domain/session.js";
import { withAgentPicker } from "./agentPicker.js";

/** Start refusals about the base branch; the server's message says what to fix. */
const BASE_CODES = new Set([
  Codes.BASE_BRANCH_NOT_FOUND,
  Codes.REMOTE_BRANCH_NOT_FOUND,
  Codes.NO_REMOTE,
  Codes.GIT_CREDENTIAL_MISSING,
  Codes.GIT_CREDENTIAL_REJECTED,
  Codes.GIT_CREDENTIAL_STORE_LOCKED,
  Codes.FETCH_FAILED,
]);

/** Start refusals about the worktree the person picked; the server's sentence is shown beside the picker. */
const WORKTREE_CODES = new Set([Codes.WORKTREE_NOT_FOUND, Codes.WORKTREE_NOT_ON_TASK, Codes.WORKTREE_WITH_BASE_BRANCH, Codes.ACCESS_INVALID]);

/** The modes that read the work they join: a Reviewer and QA do not write it. */
const READ_ONLY_MODES = new Set(["review", "qa"]);

/** The modes that take no worktree of their own to join: an architect and the product brainstorm. */
const NO_JOIN_MODES = new Set(["architect", "product"]);

/** How each mode is named in the access hint. */
const MODE_WORDS = { review: "a Reviewer", qa: "QA", test: "a Tester", operate: "an Operator", implement: "an implementer" };

/** What an access allows, in the words the picker uses. */
const accessWord = (access) => (access === "read_only" ? "read-only" : "write");

/** "ended 2h ago" from the end time; "ended" when the server sent none. */
export function endedText(endedAt, at) {
  if (!(endedAt instanceof Date)) return "ended";
  const minutes = Math.floor((at - endedAt) / 60000);
  if (minutes < 1) return "ended just now";
  if (minutes < 60) return `ended ${minutes}m ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `ended ${hours}h ago`;
  const days = Math.floor(hours / 24);
  return days === 1 ? "ended yesterday" : `ended ${days}d ago`;
}

/** @param {{ gateway: import("../../domain/ports.js").SessionGateway }} deps */
export const newSession = ({ gateway, debounceMs = 300, location = globalThis.location, now = () => new Date() }) => (projectId = "", ticketId = "", lockedMode = "", bringUnder = []) => withAgentPicker({
  projectId,
  ticketId,
  prompt: "",
  agentId: "",
  /** "", "architect", "implement", "design" or "product": a role on top of the agent. */
  mode: lockedMode,
  /** The mode the page fixed ("product" with no task, "architect" over a selection); "" when the form offers every mode. */
  lockedMode,
  /** The sessions of the task the page selected: starting the architect moves them under it. */
  bringUnder: [...bringUnder],
  /** Every mode's model, from the server (mode value → model); the Model select starts on the chosen mode's. */
  modeModels: {},
  /** The Model select's value: the mode's model while the person has not picked one. */
  model: "",
  /** The person picked a model: the select keeps it when the mode changes. */
  modelPicked: false,
  repositoryId: "",
  autoAccept: "off",
  /** @type {import("../../domain/ports.js").Branch[]} */
  branches: [],
  baseBranch: "",
  /** The branch a fetch put the field back on; the note about it goes when the person picks another. */
  fallbackBase: "",
  loadingBranches: false,
  branchesFailed: false,
  /** A fetch from origin runs; the id of the newest one, so a superseded answer is dropped. */
  fetching: false,
  fetchAsked: 0,
  /** The server's sentence for a failed fetch, shown in the field's error row; null when none. */
  fetchError: null,
  /** Said under the field when a fetch pruned the person's pick. */
  fetchNote: "",
  /** The server's sentence for a start refused for the base, and the base it was refused for. */
  baseRefusedText: "",
  baseRefusedFor: "",
  /** The session branch's name; prefilled with the generated one, replaced by what the person types. */
  branchName: "",
  /** True once the person typed in the field: a new repository no longer replaces it. */
  branchNameEdited: false,
  /** @type {import("../../domain/ports.js").BranchNameCheck | null} */
  nameCheck: null,
  /** A check is waiting to run or is in flight. */
  checkingName: false,
  /** The check could not be made (the server did not answer). */
  nameCheckFailed: false,
  /** The pending check's timer, and the id of the newest question put to the server. */
  nameTimer: 0,
  nameAsked: 0,
  /** "Where it works": "branch" cuts a new branch, "worktree" joins one of the task's worktrees. */
  where: "branch",
  /** The task's worktrees in this repository, newest first, as the server lists them. */
  worktrees: [],
  /** The worktree picked to join; "" cuts a new branch. */
  worktreeId: "",
  /** "write" or "read_only": what the joined session may do; preset by the mode until the person flips it. */
  access: "write",
  /** The person flipped the access: the mode no longer presets it. */
  accessPicked: false,
  /** The server's sentence for a refused worktree or access, without its code; "" when none. */
  worktreeError: "",
  /** The refusal's code, shown small under the sentence. */
  worktreeErrorCode: "",
  /** The id of the newest list asked for, so a superseded answer is dropped. */
  worktreesAsked: 0,
  submitting: false,
  error: null,

  /** The agent a link asked for (?agent=<name>); "" when none, or once it is chosen. */
  wantedAgent: "",

  init() {
    // The markup may lock the mode (data-locked-mode="product" on the project's sessions page).
    const locked = this.$el.dataset.lockedMode ?? "";
    if (locked) {
      this.lockedMode = locked;
      this.mode = locked;
    }
    this.modeModels = JSON.parse(this.$el.dataset.modeModels || "{}");
    this.model = this.modeModel;
    this.$watch("mode", () => this.followMode());
    this.repositoryId = this.$el.dataset.defaultRepository ?? "";
    this.startAgentPicker(this.$el.dataset);
    // A link such as the Agents page's Start a session as <agent> names the agent: chosen once it is one the project enables.
    this.wantedAgent = new URLSearchParams(location?.search ?? "").get("agent") ?? "";
    this.chooseWantedAgent();
    this.$nextTick(() => this.$refs.prompt?.focus());
    this.$watch("baseBranch", () => this.scheduleNameCheck());
    this.$watch("baseBranch", () => this.clearFetchNote());
    this.loadBranches();
    this.loadWorktrees();
    this.checkNameNow();
  },

  destroy() {
    this.stopAgentPicker();
  },

  /** The picker read the project's enabled agents again. */
  agentsChanged() {
    this.chooseWantedAgent();
  },

  /** Selects the wanted agent when the project's enabled list has it; any other name is ignored. */
  chooseWantedAgent() {
    if (!this.wantedAgent) return;
    const match = this.agents.find((a) => a.name === this.wantedAgent);
    if (!match) return;
    this.agentId = match.id;
    this.wantedAgent = "";
  },

  get localBranches() {
    return this.branches.filter((b) => !b.remote);
  },
  get remoteBranches() {
    return this.branches.filter((b) => b.remote);
  },
  get hasRemoteBranches() {
    return this.remoteBranches.length > 0;
  },
  /** The base is a remote branch of origin: the server fetches it before the cut. */
  get remoteBase() {
    return this.baseBranch.startsWith("origin/") && this.branches.some((b) => b.remote && b.name === this.baseBranch);
  },
  /** What the worktree will branch off, in words, under the picker. */
  get baseHint() {
    if (this.fetching) return "Fetching from origin…";
    if (this.fetchNote) return this.fetchNote;
    if (this.loadingBranches) return "Loading the repository's branches…";
    if (this.branchesFailed) return "Could not list the branches; the session branches off what the checkout has checked out.";
    if (this.remoteBase) return `The session gets its own branch, cut from ${this.baseBranch}; it is fetched from origin when the session starts.`;
    const head = this.branches.find((b) => b.isHead);
    if (!this.baseBranch || this.baseBranch === head?.name) return "The session gets its own branch, cut from the branch checked out in the repository.";
    return `The session gets its own branch, cut from ${this.baseBranch}.`;
  },

  /** The Branch off combobox's error row (data-error); null removes it. */
  get branchError() {
    if (this.fetchError) return this.fetchError;
    return this.branchesFailed ? "Could not list the branches." : null;
  },
  /** The sentence on the status line under the field: a refused base, or a failed fetch. */
  get baseStatus() {
    return this.baseRefusal || this.fetchError || "";
  },
  get hasBaseStatus() {
    return this.baseStatus !== "";
  },
  /** The Branch off field is marked invalid: a start was refused for the base. */
  get baseInvalid() {
    return this.baseRefusal !== "";
  },
  /** The refusal stands while the field still holds the base it was refused for. */
  get baseRefusal() {
    return this.baseRefusedFor === this.baseBranch ? this.baseRefusedText : "";
  },
  clearBaseRefusal() {
    this.baseRefusedText = "";
  },
  /** A start refusal about the base lands on the Branch off status line, not in the banner. */
  baseRefused(err) {
    if (!BASE_CODES.has(err?.code)) return false;
    this.baseRefusedText = err.message;
    this.baseRefusedFor = this.baseBranch;
    return true;
  },
  /** The Branch off combobox's aria-busy: the list loads or a fetch runs. */
  get branchBusy() {
    return this.loadingBranches || this.fetching;
  },
  /** The combobox's Retry: the fetch when it failed, else the list. */
  retryBranches() {
    if (this.fetchError) this.fetchOrigin();
    else this.loadBranches();
  },
  clearFetchNote() {
    if (this.baseBranch !== this.fallbackBase) this.fetchNote = "";
  },

  /** Fetches origin with the stored credential, then lists the branches again; the pick stays when it still exists. */
  async fetchOrigin() {
    const repositoryId = this.repositoryId;
    if (!repositoryId || this.fetching || this.loadingBranches) return;
    const asked = ++this.fetchAsked;
    const superseded = () => asked !== this.fetchAsked || repositoryId !== this.repositoryId;
    const picked = this.baseBranch;
    this.fetching = true;
    this.fetchError = null;
    this.fetchNote = "";
    this.clearBaseRefusal();
    try {
      await gateway.fetchRemote(repositoryId);
      if (superseded()) return;
      const branches = await gateway.listBranches(repositoryId);
      if (superseded()) return;
      this.branches = branches;
      if (picked && !branches.some((b) => b.name === picked)) {
        const head = branches.find((b) => b.isHead)?.name ?? "";
        this.fallbackBase = head;
        this.baseBranch = head;
        this.fetchNote = `${picked} is not on origin any more; the field is back on the checked-out branch.`;
      }
    } catch (err) {
      if (superseded()) return;
      this.fetchError = err?.message || "Could not fetch from origin.";
    } finally {
      if (asked === this.fetchAsked) this.fetching = false;
    }
  },

  /** Lists the chosen repository's branches and preselects the checked-out one. */
  async loadBranches() {
    const repositoryId = this.repositoryId;
    this.fetchAsked++; // a fetch for the previous repository is dropped
    this.fetching = false;
    this.fetchError = null;
    this.fetchNote = "";
    this.branches = [];
    this.baseBranch = "";
    this.branchesFailed = false;
    if (!repositoryId) return;
    this.loadingBranches = true;
    try {
      const branches = await gateway.listBranches(repositoryId);
      if (repositoryId !== this.repositoryId) return; // another repository was chosen meanwhile
      this.branches = branches;
      const head = branches.find((b) => b.isHead) ?? branches[0];
      // Set once the options exist, so the <select> shows it.
      this.$nextTick(() => {
        this.baseBranch = head?.name ?? "";
      });
    } catch {
      this.branchesFailed = true;
    } finally {
      this.loadingBranches = false;
    }
  },

  /** The model the chosen mode starts with. */
  get modeModel() {
    return this.modeModels[this.mode] ?? "default";
  },
  /** The model select follows the mode until the person picks one. */
  followMode() {
    if (!this.modelPicked) this.model = this.modeModel;
    if (!this.accessPicked) this.access = this.presetAccess;
  },
  /** @param {{ target: { value: string } }} event the select's change */
  pickModel(event) {
    this.model = event.target.value;
    this.modelPicked = true;
  },
  /** The pick differs from the mode's model. */
  get modelDiffers() {
    return this.model !== this.modeModel;
  },
  /** Says what the pick overrides, beside the select. */
  get modelHint() {
    return `Mode model: ${this.modeModel}`;
  },
  /** Back to the mode's model, which the select follows again. */
  resetModel() {
    this.modelPicked = false;
    this.model = this.modeModel;
  },
  /** The product brainstorm's form is locked to the Product row: it offers no pick. */
  get showModelPicker() {
    return this.lockedMode !== "product";
  },
  /** The model the start overrides the mode's with; undefined lets the server resolve the mode's. */
  get modelOverride() {
    return this.showModelPicker && this.modelDiffers ? this.model : undefined;
  },

  get architect() {
    return this.mode === "architect";
  },
  get implement() {
    return this.mode === "implement";
  },
  get design() {
    return this.mode === "design";
  },
  get product() {
    return this.mode === "product";
  },
  get reviewing() {
    return this.mode === "review";
  },
  get testing() {
    return this.mode === "test";
  },
  get qa() {
    return this.mode === "qa";
  },
  get operate() {
    return this.mode === "operate";
  },
  /** The form starts an architect over sessions the page selected. */
  get bringingUnder() {
    return this.bringUnder.length > 0;
  },
  get heading() {
    return this.bringingUnder ? "Start an architect" : "New session";
  },
  get lockedModeWord() {
    const n = this.bringUnder.length;
    return `Architect — it supervises the ${n === 1 ? "session" : `${n} sessions`} you selected.`;
  },
  get promptPlaceholder() {
    if (this.bringingUnder) return "Optional — added after the architect's opening, which names the sessions you selected. Add anything it should know.";
    if (this.architect) return "Optional — the architect starts from the task's title and description. Add anything it should know.";
    if (this.implement) return "Which spec should it implement? Name its Spec task document, e.g. \"Spec: backend — CSV export\".";
    if (this.design) return "What should it design? Components, screens, images or videos: each one it publishes appears in the Design tab.";
    if (this.product) return "Optional — what is the feature about? The agent interviews you from here before writing anything.";
    return "What should the agent do on this task? Leave empty to open claude and type in its terminal.";
  },
  get modeHint() {
    if (this.bringingUnder) return "The sessions you selected move under it: each keeps its branch, worktree and conversation, and reports to the architect from now on. It supervises them instead of grilling you and writing specs.";
    if (this.architect) return "Adds the task-architecture skill: it grills you on the design, writes per-application specs as task documents and starts an implement session for each.";
    if (this.implement) return "Adds the task-implement-spec skill: it writes tickets for the architect to review, then implements them test first on its branch.";
    if (this.design) return "Adds the design skill: the agent publishes pages, images and videos as it works; they show live in the Design tab.";
    if (this.product) return "Adds the task-product skill: it interviews you, writes the PRD as a task document and creates one child task per piece of work, each with a brief. It never writes code.";
    if (this.reviewing) return "Adds the task-review skill: it reviews the branch it starts from, or a pull request, and writes a Review with approve or request changes. Name the branch under check in Branch off.";
    if (this.testing) return "Adds the task-test skill: it writes tests for the branch under check on its own branch, runs them and writes a Test report. Name the branch under check in Branch off.";
    if (this.qa) return "Adds the task-qa skill: it walks the use cases in the running application and publishes screenshots and videos to the Design tab. Name the branch under check in Branch off.";
    if (this.operate) return "Adds the task-operate skill: it runs, configures and checks the services for the task's branch, and each change waits for your approval.";
    return "Runs the agent as it is.";
  },

  repositoryChanged() {
    this.useNewBranch();
    this.loadBranches();
    this.loadWorktrees();
    this.checkNameNow();
  },

  // ── joining another session's worktree ───────────────────────────────
  /** Lists the task's worktrees in the chosen repository; a failure leaves the picker empty, and the form cuts a branch as today. */
  async loadWorktrees() {
    const repositoryId = this.repositoryId;
    const asked = ++this.worktreesAsked;
    this.worktrees = [];
    if (!repositoryId || !this.ticketId) return;
    try {
      const listed = await gateway.taskWorktrees({ ticketId: this.ticketId, repositoryId });
      if (asked !== this.worktreesAsked) return;
      this.worktrees = listed.worktrees;
      // A pick that is not on the new list, or an empty list, falls back to a new branch.
      if (this.worktreeId !== "" && !this.worktrees.some((w) => w.worktreeId === this.worktreeId)) this.worktreeId = "";
      if (this.where === "worktree" && this.noWorktrees) this.where = "branch";
    } catch {
      if (asked === this.worktreesAsked) this.worktrees = [];
    }
  },
  /** The mode's access: a Reviewer and QA read-only, every other mode write. */
  get presetAccess() {
    return READ_ONLY_MODES.has(this.mode) ? "read_only" : "write";
  },
  /** "Where it works" is offered where a join can happen: on a task, in a mode that takes a worktree. */
  get showWorktreePicker() {
    return this.ticketId !== "" && !NO_JOIN_MODES.has(this.mode);
  },
  /** "An existing worktree" is chosen, picked or not: the Worktree and Access fields and their refusals are on screen, and the branch fields are not. */
  get existingChosen() {
    return this.showWorktreePicker && this.where === "worktree";
  },
  /** "An existing worktree" is chosen and one is picked: the start joins instead of cutting a branch. */
  get joining() {
    return this.existingChosen && this.worktreeId !== "";
  },
  /** The base branch and the branch name apply only to a session that cuts its own branch. */
  get showBranchFields() {
    return !this.existingChosen;
  },
  get newBranchChosen() {
    return !this.existingChosen;
  },
  /** The radio card "An existing worktree" has nothing to offer. */
  get noWorktrees() {
    return this.worktrees.length === 0;
  },
  /** The second card's description: what it does and how many there are, or why it is disabled. */
  get joinCardText() {
    if (this.noWorktrees) return `No session of this task has a worktree in ${this.repositoryLabel} yet. Start one on a new branch first.`;
    return `Join a session of this task on its branch. ${this.worktrees.length} to choose from.`;
  },
  /** The repository the form is on, by name. */
  get repositoryLabel() {
    return this.repositoryName || "this repository";
  },
  /** The picker's options: one row per worktree: the creator's agent and mode, the branch and who else is on it, running or when it ended. */
  get worktreeOptions() {
    return this.worktrees.map((w) => {
      const creator = w.sessions.find((x) => x.sessionId === w.createdBySessionId) ?? w.sessions[0];
      const others = w.sessions.filter((x) => x !== creator);
      const withText = others.length ? `with ${others.map((x) => `${x.agent || "claude"} (${accessWord(x.access)})`).join(", ")}` : "nobody else";
      const running = creator?.running === true;
      return {
        id: w.worktreeId,
        label: `${creator?.agent || "claude"} · ${creator?.mode || "default"}`,
        description: `${w.branch} · ${withText}`,
        meta: running ? "running" : endedText(creator?.endedAt, now()),
        tone: running ? "signal" : "done",
        title: creator ? `session ${creator.sessionId}` : "",
      };
    });
  },
  /** The chosen worktree, as a picker row. */
  get pickedWorktree() {
    return this.worktrees.find((w) => w.worktreeId === this.worktreeId) ?? null;
  },
  /** Under the picker: who is on the chosen worktree now and their access; informational, nothing is locked. */
  get worktreeHint() {
    const w = this.pickedWorktree;
    if (!w) return `Worktrees of this task in ${this.repositoryLabel}, still on disk.`;
    const who = w.sessions.map((x) => `${x.agent || "claude"} (${x.mode || "default"}) is ${x.running ? "running" : "ended"} on it with ${accessWord(x.access)} access`).join("; ");
    return `${who}. They coordinate through commits; nothing is locked.`;
  },
  /** The prompt's hint when joining: the outcome in one sentence. */
  get joinOutcome() {
    const w = this.pickedWorktree;
    const owner = w?.sessions.find((x) => x.sessionId === w.createdBySessionId)?.agent || "claude";
    const how = this.access === "read_only" ? "read-only" : "with write access";
    return `claude starts in a terminal on the server, ${how} on ${owner}'s worktree, branch ${w?.branch ?? ""}.`;
  },
  /** The radio cards' change event. */
  whereChosen(event) {
    this.chooseWhere(event.target.value);
  },
  /** "branch" cuts a new branch; "worktree" joins the newest worktree of the task until the person picks another. */
  chooseWhere(where) {
    if (where === "worktree" && !this.noWorktrees) {
      this.pickWorktree(this.worktreeId || this.worktrees[0].worktreeId);
    } else if (where !== "worktree") {
      this.useNewBranch();
    }
  },
  /** Back to cutting a new branch: the worktree pick and its refusal go. */
  useNewBranch() {
    this.where = "branch";
    this.worktreeId = "";
    this.worktreeError = "";
    this.worktreeErrorCode = "";
  },
  /** The picker's change event. */
  worktreePicked(event) {
    this.pickWorktree(event.target.value);
  },
  /** Picks a worktree to join; the access follows the mode until the person flips it. */
  pickWorktree(id) {
    this.worktreeId = id;
    if (id !== "") this.where = "worktree";
    this.worktreeError = "";
    this.worktreeErrorCode = "";
    if (!this.accessPicked) this.access = this.presetAccess;
  },
  /** The access radios' change event. */
  accessChosen(event) {
    this.pickAccess(event.target.value);
  },
  /** @param {"write"|"read_only"} access */
  pickAccess(access) {
    this.access = access;
    this.accessPicked = true;
    this.worktreeError = "";
    this.worktreeErrorCode = "";
  },
  /** Back to the mode's access, which follows the mode again. */
  resetAccess() {
    this.accessPicked = false;
    this.access = this.presetAccess;
  },
  get hasWorktreeError() {
    return this.worktreeError !== "";
  },
  get writeAccess() {
    return this.access === "write";
  },
  get readOnlyAccess() {
    return this.access === "read_only";
  },
  /** The hint under the Access radios: the mode's default, and what it means. */
  get accessHint() {
    const word = MODE_WORDS[this.mode];
    if (this.presetAccess === "read_only") {
      return `Read-only is the default for ${word}. Builds and tests still run in the worktree; every write is denied.`;
    }
    return word ? `Write is the default for ${word}: it commits on the shared branch.` : "Write is the default: it edits and commits on the shared branch.";
  },
  /** A refusal about the worktree lands beside the picker, not in the banner; a vanished one is dropped from it. */
  worktreeRefusal(err) {
    if (!WORKTREE_CODES.has(err?.code)) return false;
    const sentence = String(err.message ?? "").replace(/^[A-Z_]+: /, "");
    if (err.code === Codes.WORKTREE_NOT_FOUND || err.code === Codes.WORKTREE_NOT_ON_TASK) {
      const gone = err.details?.worktree_id ?? this.worktreeId;
      this.worktrees = this.worktrees.filter((w) => w.worktreeId !== gone);
      // The form stays on An existing worktree with no pick: the sentence has
      // to be seen, and Start waits for another pick or a deliberate switch.
      if (this.worktreeId === gone) this.worktreeId = "";
    }
    this.worktreeError = sentence;
    this.worktreeErrorCode = err.code;
    return true;
  },

  // ── the session branch's name ────────────────────────────────────────
  /** The person typed in the Branch name field. */
  nameTyped() {
    this.branchNameEdited = true;
    this.scheduleNameCheck();
  },
  /** Checks the name once typing pauses; every call restarts the wait. */
  scheduleNameCheck() {
    if (!this.repositoryId) return;
    clearTimeout(this.nameTimer);
    this.checkingName = true;
    this.nameTimer = setTimeout(() => this.checkNameNow(), debounceMs);
  },
  /** Asks the server about the name now; an answer to a superseded question is dropped. */
  async checkNameNow() {
    clearTimeout(this.nameTimer);
    const repositoryId = this.repositoryId;
    if (!repositoryId) return;
    const asked = ++this.nameAsked;
    const name = this.branchName.trim();
    this.checkingName = true;
    this.nameCheckFailed = false;
    try {
      const check = await gateway.checkBranchName({ repositoryId, name, baseBranch: this.baseBranch, ticketId: this.ticketId });
      if (asked !== this.nameAsked) return;
      this.nameCheck = check;
      if (!this.branchNameEdited && check.generated) this.branchName = check.generated;
    } catch {
      if (asked !== this.nameAsked) return;
      this.nameCheck = null;
      this.nameCheckFailed = true;
    } finally {
      if (asked === this.nameAsked) this.checkingName = false;
    }
  },
  /** One click on the chip: the suggested free name replaces the taken one. */
  useSuggestion() {
    const suggestion = this.nameCheck?.suggestion;
    if (!suggestion) return;
    this.branchName = suggestion;
    this.branchNameEdited = true;
    this.checkNameNow();
  },
  /** Selects the prefilled name when the field gets focus, so typing replaces it. */
  selectName(event) {
    event.target.select();
  },

  /** "checking", "unchecked", "generated", "free", "taken", "invalid", or "" before the first answer. */
  get nameState() {
    if (this.checkingName) return "checking";
    if (this.nameCheckFailed) return "unchecked";
    const c = this.nameCheck;
    if (!c) return "";
    if (c.code === Codes.BRANCH_EXISTS) return "taken";
    if (c.code === Codes.BRANCH_NAME_INVALID) return "invalid";
    return c.name === "" ? "generated" : "free";
  },
  /** The line under the field; the same words whether the check or the start said it. */
  get nameMessage() {
    const c = this.nameCheck;
    switch (this.nameState) {
      case "checking":
        return "Checking the branch name…";
      case "unchecked":
        return "Could not check the name; the server checks it again when the session starts.";
      case "free":
        return "Free on this repository";
      case "generated":
        return "The server generates a name when this is empty.";
      case "taken":
        return `${c.name} already exists on this repository. An existing branch is never reused.`;
      case "invalid":
        return `Not a valid branch name: ${c.reason.replace(/\.$/, "")}.`;
      default:
        return "";
    }
  },
  /** The design system's .error for a refused name, .hint otherwise. */
  get nameMessageClass() {
    return this.nameInvalid ? "error" : "hint";
  },
  get nameSuggestion() {
    return this.nameState === "taken" ? this.nameCheck.suggestion : "";
  },
  get nameInvalid() {
    return this.nameState === "taken" || this.nameState === "invalid";
  },
  /** What the field's hint says it becomes. */
  get nameHint() {
    return `Becomes the worktree's branch. Cut from ${this.baseBranch || "the checked-out branch"}.`;
  },
  /** A start refusal about the name lands on the field, not in the banner. */
  nameRefusal(err) {
    if (err?.code !== Codes.BRANCH_EXISTS && err?.code !== Codes.BRANCH_NAME_INVALID) return false;
    this.nameCheck = Object.freeze({
      name: err.details?.name ?? this.branchName.trim(),
      generated: this.nameCheck?.generated ?? "",
      valid: err.code === Codes.BRANCH_EXISTS,
      available: false,
      code: err.code,
      reason: err.details?.reason ?? "",
      suggestion: err.details?.suggestion ?? "",
    });
    this.checkingName = false;
    this.nameCheckFailed = false;
    return true;
  },

  /** Why the draft cannot start: no repository, or no task in a mode that needs one. */
  get problem() {
    if (this.repositoryId === "") return "Pick a repository.";
    if (this.noAgentEnabled) return this.noAgentLine;
    if (this.ticketId === "") return noTaskProblem(this.mode);
    if (this.existingChosen && this.worktreeId === "") return "Pick a worktree.";
    if (this.joining) return "";
    if (this.nameState === "checking") return "Checking the branch name…";
    if (this.nameState === "taken") return "The branch name is taken. Pick another.";
    if (this.nameState === "invalid") return "The branch name is not valid.";
    return "";
  },
  get cannotSubmit() {
    return this.submitting || this.fetching || this.problem !== "";
  },

  async submit() {
    if (this.cannotSubmit) return;
    this.submitting = true;
    this.error = null;
    this.clearBaseRefusal();
    this.agentRefusal = null;
    this.worktreeError = "";
    this.worktreeErrorCode = "";
    const joining = this.joining;
    try {
      const session = await gateway.start({
        projectId: this.projectId,
        ticketId: this.ticketId,
        repositoryId: this.repositoryId,
        agentId: this.agentId,
        mode: this.mode,
        prompt: this.prompt.trim(),
        autoAccept: this.autoAccept,
        baseBranch: joining ? undefined : this.baseBranch,
        branchName: joining ? undefined : this.branchName.trim(),
        worktreeId: joining ? this.worktreeId : undefined,
        access: joining ? this.access : undefined,
        model: this.modelOverride,
        size: INITIAL_TERMINAL_SIZE,
        bringUnderSessionIds: this.bringingUnder ? this.bringUnder : undefined,
      });
      this.$dispatch("session-created", { session });
    } catch (err) {
      if (!this.refuseAgent(err) && !this.worktreeRefusal(err) && !this.nameRefusal(err) && !this.baseRefused(err)) this.error = describeError(err);
    } finally {
      this.submitting = false;
    }
  },

  cancel() {
    this.$dispatch("new-session-cancelled");
  },
}, { gateway });
