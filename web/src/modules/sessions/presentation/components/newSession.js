// The new-session form: starts an interactive session on the page's task,
// in a terminal on the server; the page then shows that terminal.
// The server renders the choices (agents, repositories); this holds the
// draft and submits it. The page passes its project and task in and listens
// for session-created / new-session-cancelled.
//
//   <form x-data="sessionsNewSession(projectId, ticketId)"
//         data-default-repository="r1" x-on:submit.prevent="submit">

import { describeError } from "../../../../shared/presentation/errors.js";
import { INITIAL_TERMINAL_SIZE } from "../../domain/session.js";

/** @param {{ gateway: import("../../domain/ports.js").SessionGateway }} deps */
export const newSession = ({ gateway }) => (projectId = "", ticketId = "") => ({
  projectId,
  ticketId,
  prompt: "",
  agentId: "",
  /** "", "architect" or "design": a role on top of the agent. */
  mode: "",
  repositoryId: "",
  autoAccept: "off",
  /** @type {import("../../domain/ports.js").Branch[]} */
  branches: [],
  baseBranch: "",
  loadingBranches: false,
  branchesFailed: false,
  submitting: false,
  error: null,

  init() {
    this.repositoryId = this.$el.dataset.defaultRepository ?? "";
    this.$nextTick(() => this.$refs.prompt?.focus());
    this.loadBranches();
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
  get design() {
    return this.mode === "design";
  },
  get promptPlaceholder() {
    if (this.architect) return "Optional — the architect starts from the task's title and description. Add anything it should know.";
    if (this.design) return "What should it design? Components, screens, images or videos: each one it publishes appears in the Design tab.";
    return "What should the agent do on this task? Leave empty to open claude and type in its terminal.";
  },
  get modeHint() {
    if (this.architect) return "Adds the task-architecture skill: it writes per-application specs as task documents and starts a planning session for each.";
    if (this.design) return "Adds the design skill: the agent publishes pages, images and videos as it works; they show live in the Design tab.";
    return "Runs the agent as it is.";
  },

  repositoryChanged() {
    this.loadBranches();
  },

  get cannotSubmit() {
    return this.submitting || this.repositoryId === "";
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
        size: INITIAL_TERMINAL_SIZE,
      });
      this.$dispatch("session-created", { session });
    } catch (err) {
      this.error = describeError(err);
    } finally {
      this.submitting = false;
    }
  },

  cancel() {
    this.$dispatch("new-session-cancelled");
  },
});
