// A repository's run commands, on its settings page: every saved command
// with its command line, edited in place (saving a name again replaces its
// command line), removed after a confirm, and a form to add one. The names
// follow the App tab's rule, and a refused one is explained under its field.
//
//   <section x-data="runsCommandsSettings" data-seed="run-commands-seed"> …

import { Codes, codeOf } from "../../../../shared/domain/errors.js";
import { describeError } from "../../../../shared/presentation/errors.js";
import { readSeed } from "../../../../shared/presentation/seed.js";
import { runCommandNameProblem } from "../../domain/run.js";

/** @param {{ gateway: import("../../domain/ports.js").RunGateway }} deps */
export const runCommandsSettings = ({ gateway }) => () => ({
  repositoryId: "",
  /** @type {{ name: string, command: string, draft: string, saving: boolean, confirming: boolean, removing: boolean }[]} */
  commands: [],
  newName: "",
  newCommand: "",
  /** The server's refusal of a name, kept while the name field still holds it. */
  refused: { name: "", message: "" },
  adding: false,
  ready: false,
  error: null,
  notice: "",

  get isEmpty() {
    return this.ready && this.commands.length === 0;
  },
  get newNameProblem() {
    if (!this.newName.trim()) return "";
    if (this.refused.name !== "" && this.refused.name === this.newName) return this.refused.message;
    const problem = runCommandNameProblem(this.newName);
    if (problem) return problem;
    return this.commands.some((c) => c.name === this.newName.trim())
      ? "That command is already here; change its command line above."
      : "";
  },
  get cannotAdd() {
    return !this.ready || this.adding || !this.newName.trim() || !this.newCommand.trim() || this.newNameProblem !== "";
  },

  init() {
    try {
      const seed = gateway.decodeCommandsSeed(readSeed(this.$el));
      this.repositoryId = seed.repositoryId;
      this.commands = seed.commands.map(toRow);
    } catch (err) {
      this.error = describeError(err);
      return;
    }
    this.$nextTick(() => {
      for (const node of this.$el.querySelectorAll("[data-ssr]")) node.remove();
      this.ready = true;
    });
  },

  cannotSave(name) {
    const c = this.commands.find((x) => x.name === name);
    return !c || c.saving || !c.draft.trim() || c.draft === c.command;
  },

  async save(name) {
    const c = this.commands.find((x) => x.name === name);
    if (!c || this.cannotSave(name)) return;
    c.saving = true;
    this.error = null;
    this.notice = "";
    try {
      Object.assign(c, toRow(await gateway.saveCommand(this.repositoryId, c.name, c.draft)));
      this.notice = `Saved “${c.name}”. Its next run uses the new command line.`;
    } catch (err) {
      this.error = describeError(err);
    } finally {
      c.saving = false;
    }
  },

  revert(name) {
    const c = this.commands.find((x) => x.name === name);
    if (c) c.draft = c.command;
  },

  async add() {
    if (this.cannotAdd) return;
    const name = this.newName;
    this.adding = true;
    this.error = null;
    this.notice = "";
    try {
      const saved = await gateway.saveCommand(this.repositoryId, name.trim(), this.newCommand);
      this.commands = [...this.commands.filter((c) => c.name !== saved.name), toRow(saved)].sort((a, b) =>
        a.name.localeCompare(b.name),
      );
      this.newName = "";
      this.newCommand = "";
      this.notice = `Added “${saved.name}”. Sessions of this repository offer it in their App tab.`;
    } catch (err) {
      if (codeOf(err) === Codes.INVALID_NAME) this.refused = { name, message: describeError(err).message };
      else this.error = describeError(err);
    } finally {
      this.adding = false;
    }
  },

  askRemove(name) {
    for (const c of this.commands) c.confirming = c.name === name;
  },
  cancelRemove() {
    for (const c of this.commands) c.confirming = false;
  },

  async remove(name) {
    const c = this.commands.find((x) => x.name === name);
    if (!c || c.removing) return;
    c.removing = true;
    this.error = null;
    this.notice = "";
    try {
      await gateway.deleteCommand(this.repositoryId, name);
      this.commands = this.commands.filter((x) => x.name !== name);
      this.notice = `Removed “${name}”.`;
    } catch (err) {
      this.error = describeError(err);
      c.removing = false;
    }
  },

  dismiss() {
    this.error = null;
    this.notice = "";
  },
});

function toRow(c) {
  return { name: c.name, command: c.command, draft: c.command, saving: false, confirming: false, removing: false };
}
