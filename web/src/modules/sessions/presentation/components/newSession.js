// The new-session form: starts an interactive session on the page's task,
// in a terminal on the server; the page then shows that terminal.
// The server renders the choices (agents, repositories); this holds the
// draft and submits it. The page passes its project and task in and listens
// for session-created / new-session-cancelled.
//
//   <form x-data="sessionsNewSession(projectId, ticketId)"
//         data-default-repository="r1" x-on:submit.prevent="submit">

import { describeError } from "../../../../shared/presentation/errors.js";

/** @param {{ gateway: import("../../domain/ports.js").SessionGateway }} deps */
export const newSession = ({ gateway }) => (projectId = "", ticketId = "") => ({
  projectId,
  ticketId,
  prompt: "",
  agentId: "",
  repositoryId: "",
  autoAccept: "off",
  submitting: false,
  error: null,

  init() {
    this.repositoryId = this.$el.dataset.defaultRepository ?? "";
    this.$nextTick(() => this.$refs.prompt?.focus());
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
        prompt: this.prompt.trim(),
        autoAccept: this.autoAccept,
        // The size it starts at; the terminal pane resizes it once attached.
        size: { cols: 120, rows: 32 },
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
