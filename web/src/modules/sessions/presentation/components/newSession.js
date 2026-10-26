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

/** @param {{ gateway: import("../../domain/ports.js").SessionGateway }} deps */
export const newSession = ({ gateway, debounceMs = 300 }) => (projectId = "", ticketId = "", lockedMode = "") => ({
  projectId,
  ticketId,
  prompt: "",
  agentId: "",
  /** "", "architect", "implement", "design" or "product": a role on top of the agent. */
  mode: lockedMode,
  /** The mode the page fixed ("product" with no task); "" when the form offers every mode. */
  lockedMode,
  repositoryId: "",
  autoAccept: "off",
  /** @type {import("../../domain/ports.js").Branch[]} */
  branches: [],
  baseBranch: "",
  loadingBranches: false,
  branchesFailed: false,
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
  submitting: false,
  error: null,

  init() {
    // The markup may lock the mode (data-locked-mode="product" on the project's sessions page).
    const locked = this.$el.dataset.lockedMode ?? "";
    if (locked) {
      this.lockedMode = locked;
      this.mode = locked;
    }
    this.repositoryId = this.$el.dataset.defaultRepository ?? "";
    this.$nextTick(() => this.$refs.prompt?.focus());
    this.$watch("baseBranch", () => this.scheduleNameCheck());
    this.loadBranches();
    this.checkNameNow();
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
  /** What the worktree will branch off, in words, under the picker. */
  get baseHint() {
    if (this.loadingBranches) return "Loading the repository's branches…";
    if (this.branchesFailed) return "Could not list the branches; the session branches off what the checkout has checked out.";
    const head = this.branches.find((b) => b.isHead);
    if (!this.baseBranch || this.baseBranch === head?.name) return "The session gets its own branch, cut from the branch checked out in the repository.";
    return `The session gets its own branch, cut from ${this.baseBranch}.`;
  },

  /** The Branch off combobox's error row (data-error); null removes it. */
  get branchError() {
    return this.branchesFailed ? "Could not list the branches." : null;
  },

  /** Lists the chosen repository's branches and preselects the checked-out one. */
  async loadBranches() {
    const repositoryId = this.repositoryId;
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
  get promptPlaceholder() {
    if (this.architect) return "Optional — the architect starts from the task's title and description. Add anything it should know.";
    if (this.implement) return "Which spec should it implement? Name its Spec task document, e.g. \"Spec: backend — CSV export\".";
    if (this.design) return "What should it design? Components, screens, images or videos: each one it publishes appears in the Design tab.";
    if (this.product) return "Optional — what is the feature about? The agent interviews you from here before writing anything.";
    return "What should the agent do on this task? Leave empty to open claude and type in its terminal.";
  },
  get modeHint() {
    if (this.architect) return "Adds the task-architecture skill: it grills you on the design, writes per-application specs as task documents and starts an implement session for each.";
    if (this.implement) return "Adds the task-implement-spec skill: it writes tickets for the architect to review, then implements them test first on its branch.";
    if (this.design) return "Adds the design skill: the agent publishes pages, images and videos as it works; they show live in the Design tab.";
    if (this.product) return "Adds the task-product skill: it interviews you, writes the PRD as a task document and creates one child task per piece of work, each with a brief. It never writes code.";
    return "Runs the agent as it is.";
  },

  repositoryChanged() {
    this.loadBranches();
    this.checkNameNow();
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
    if (this.ticketId === "") return noTaskProblem(this.mode);
    if (this.nameState === "checking") return "Checking the branch name…";
    if (this.nameState === "taken") return "The branch name is taken. Pick another.";
    if (this.nameState === "invalid") return "The branch name is not valid.";
    return "";
  },
  get cannotSubmit() {
    return this.submitting || this.problem !== "";
  },

  async submit() {
    if (this.cannotSubmit) return;
    this.submitting = true;
    this.error = null;
    try {
      const session = await gateway.start({
        projectId: this.projectId,
        ticketId: this.ticketId,
        repositoryId: this.repositoryId,
        agentId: this.agentId,
        mode: this.mode,
        prompt: this.prompt.trim(),
        autoAccept: this.autoAccept,
        baseBranch: this.baseBranch,
        branchName: this.branchName.trim(),
        size: INITIAL_TERMINAL_SIZE,
      });
      this.$dispatch("session-created", { session });
    } catch (err) {
      if (!this.nameRefusal(err)) this.error = describeError(err);
    } finally {
      this.submitting = false;
    }
  },

  cancel() {
    this.$dispatch("new-session-cancelled");
  },
});
