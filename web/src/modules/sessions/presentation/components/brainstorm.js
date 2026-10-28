// "Brainstorm a feature" on the project board: a dialog with the agent and
// the repository, whose Start opens a product-mode session with no task and
// goes to it on the project's sessions page. The board (tasks module) only
// hosts the markup; starting a session is this module's port.
//
//   <span x-data="sessionsBrainstorm" data-project-id="p1" data-default-repository="r1">
//     <button type="button" x-on:click="open">Brainstorm a feature</button>
//     <div class="[ popup ]" role="dialog" id="brainstorm" x-data="popupDialog" x-on:dialog-open.window="openFrom($event)">
//       <form x-on:submit.prevent="start"> … <select x-model="agentId"> <select x-model="repositoryId"> …

import { describeError } from "../../../../shared/presentation/errors.js";
import { brainstormProblem, brainstormRequest } from "../../domain/brainstorm.js";
import { projectSessionsHref } from "../view.js";
import { withAgentPicker } from "./agentPicker.js";

/** The dialog's element id, which dialog-open names. */
export const BRAINSTORM_DIALOG = "brainstorm";

/** @param {{ gateway: import("../../domain/ports.js").SessionGateway, navigate: (url: string) => void }} deps */
export const brainstorm = ({ gateway, navigate }) => () => withAgentPicker({
  projectId: "",
  agentId: "",
  repositoryId: "",
  starting: false,
  error: null,

  init() {
    this.projectId = this.$el.dataset.projectId ?? "";
    this.repositoryId = this.$el.dataset.defaultRepository ?? "";
    this.startAgentPicker(this.$el.dataset);
  },

  destroy() {
    this.stopAgentPicker();
  },

  /** The trigger was pressed: opens the dialog anchored to it. */
  open(event) {
    this.$dispatch("dialog-open", { id: BRAINSTORM_DIALOG, anchor: event?.currentTarget ?? event?.target ?? null });
  },

  get problem() {
    if (this.noAgentEnabled) return this.noAgentLine;
    return brainstormProblem({ repositoryId: this.repositoryId });
  },
  get cannotStart() {
    return this.starting || this.problem !== "";
  },

  /** Starts the session and goes to it; a refusal stays in the dialog with the coded error. */
  async start() {
    if (this.cannotStart) return;
    this.starting = true;
    this.error = null;
    this.agentRefusal = null;
    try {
      const session = await gateway.start(brainstormRequest({ projectId: this.projectId, agentId: this.agentId, repositoryId: this.repositoryId }));
      navigate(projectSessionsHref(this.projectId, session.id));
    } catch (err) {
      if (!this.refuseAgent(err)) this.error = describeError(err);
    } finally {
      this.starting = false;
    }
  },
}, { gateway });
