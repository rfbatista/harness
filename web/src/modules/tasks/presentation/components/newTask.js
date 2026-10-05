// The new-task form: a title, a description (the brief its sessions start
// from), a status. On success it opens the task.
//
//   <form x-data="tasksNewTask" data-project-id="p1" x-on:submit.prevent="submit">

import { describeError } from "../../../../shared/presentation/errors.js";
import { titleProblem } from "../../domain/task.js";

/**
 * @param {{
 *   gateway: import("../../domain/ports.js").TaskGateway,
 *   navigate: (url: string) => void,
 * }} deps
 */
export const newTask = ({ gateway, navigate }) => (projectId = "") => ({
  projectId,
  title: "",
  description: "",
  status: "todo",
  submitting: false,
  error: null,

  init() {
    if (!this.projectId) this.projectId = this.$el.dataset.projectId ?? "";
  },

  get cannotSubmit() {
    return this.submitting || titleProblem(this.title) !== "";
  },

  async submit() {
    if (this.cannotSubmit) return;
    this.submitting = true;
    this.error = null;
    try {
      const task = await gateway.createTask({
        projectId: this.projectId,
        title: this.title.trim(),
        description: this.description.trim(),
        status: this.status,
      });
      navigate(`/projects/${encodeURIComponent(this.projectId)}/tasks/${encodeURIComponent(task.id)}`);
    } catch (err) {
      this.error = describeError(err);
    } finally {
      this.submitting = false;
    }
  },
});
