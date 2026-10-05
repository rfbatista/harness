// A repository's env files: the files of variables git does not carry (.env
// and the like) that the harness writes into every new session worktree cut
// from the repository, so a session can run the application. Each is edited
// in place; new ones are imported from the checkout or started empty.
//
//   <main x-data="projectsEnvFilesPage" data-seed="env-files-seed">

import { describeError } from "../../../../shared/presentation/errors.js";
import { readSeed } from "../../../../shared/presentation/seed.js";
import { cleanEnvPath, envPathProblem } from "../../domain/project.js";

/** @param {{ gateway: import("../../domain/ports.js").ProjectGateway }} deps */
export const envFilesPage = ({ gateway }) => () => ({
  repositoryId: "",
  /** @type {{ path: string, content: string, draft: string, saving: boolean, confirming: boolean, removing: boolean }[]} */
  files: [],
  newPath: "",
  adding: false,
  error: null,
  notice: "",
  ready: false,

  get isEmpty() {
    return this.files.length === 0;
  },
  get newPathProblem() {
    if (!this.newPath.trim()) return "";
    const problem = envPathProblem(this.newPath);
    if (problem) return problem;
    return this.files.some((f) => f.path === cleanEnvPath(this.newPath)) ? "That file is already here; edit it above." : "";
  },
  get cannotAdd() {
    return !this.ready || this.adding || !this.newPath.trim() || this.newPathProblem !== "";
  },

  init() {
    try {
      const seed = gateway.decodeEnvFiles(readSeed(this.$el));
      this.repositoryId = seed.repositoryId;
      this.files = seed.files.map(toRow);
      this.newPath = this.files.some((f) => f.path === ".env") ? "" : ".env";
    } catch (err) {
      this.error = describeError(err);
      return;
    }
    this.$nextTick(() => {
      for (const node of this.$el.querySelectorAll("[data-ssr]")) node.remove();
      this.ready = true;
    });
  },

  isDirty(path) {
    const f = this.files.find((x) => x.path === path);
    return !!f && f.draft !== f.content;
  },
  cannotSave(path) {
    const f = this.files.find((x) => x.path === path);
    return !f || f.saving || f.draft === f.content;
  },

  async save(path) {
    const f = this.files.find((x) => x.path === path);
    if (!f || this.cannotSave(path)) return;
    f.saving = true;
    this.error = null;
    try {
      const saved = await gateway.saveEnvFile(this.repositoryId, f.path, f.draft);
      Object.assign(f, toRow(saved));
      this.notice = `Saved ${f.path}. New sessions get it; running ones keep what they started with.`;
    } catch (err) {
      this.error = describeError(err);
    } finally {
      f.saving = false;
    }
  },

  revert(path) {
    const f = this.files.find((x) => x.path === path);
    if (f) f.draft = f.content;
  },

  /** Imports the file at newPath from the repository's checkout. */
  async importFile() {
    await this.addWith((path) => gateway.importEnvFile(this.repositoryId, path));
  },

  /** Starts an empty file at newPath, to paste its variables into. */
  async addEmpty() {
    await this.addWith((path) => gateway.saveEnvFile(this.repositoryId, path, ""));
  },

  async addWith(create) {
    if (this.cannotAdd) return;
    this.adding = true;
    this.error = null;
    this.notice = "";
    try {
      const file = await create(cleanEnvPath(this.newPath));
      this.files = [...this.files, toRow(file)].sort((a, b) => a.path.localeCompare(b.path));
      this.newPath = "";
    } catch (err) {
      this.error = describeError(err);
    } finally {
      this.adding = false;
    }
  },

  askRemove(path) {
    for (const f of this.files) f.confirming = f.path === path;
  },
  cancelRemove() {
    for (const f of this.files) f.confirming = false;
  },

  async remove(path) {
    const f = this.files.find((x) => x.path === path);
    if (!f || f.removing) return;
    f.removing = true;
    this.error = null;
    try {
      await gateway.deleteEnvFile(this.repositoryId, path);
      this.files = this.files.filter((x) => x.path !== path);
      this.notice = `Removed ${path}. Sessions started from now on will not get it.`;
    } catch (err) {
      this.error = describeError(err);
      f.removing = false;
    }
  },

  dismiss() {
    this.error = null;
    this.notice = "";
  },
});

function toRow(file) {
  return { path: file.path, content: file.content, draft: file.content, saving: false, confirming: false, removing: false };
}
