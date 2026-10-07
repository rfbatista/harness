// One project's settings: its name and directory, the paths its file tree
// hides, and deleting it. Saving sends only what changed, so a legacy
// project whose name collides or whose directory is gone can still have its
// other field edited. A change made elsewhere (another tab, an agent, the
// TUI) arrives over the catalog feed: it updates fields the person has not
// touched, and says so when it lands under unsaved edits. Deleting asks for
// the project's name and is refused while sessions run; the refusal names
// them, with links to their tasks.
//
//   <main x-data="projectsSettingsPage" data-seed="settings-seed">

import { Codes, StructuredError, codeOf } from "../../../../shared/domain/errors.js";
import { describeError } from "../../../../shared/presentation/errors.js";
import { readSeed } from "../../../../shared/presentation/seed.js";
import { changedFields, confirmsDelete, ignoredPathProblem, isAbsolutePath } from "../../domain/project.js";

const NAME_CODES = new Set([Codes.PROJECT_NAME_TAKEN, Codes.INVALID_INPUT]);
const ROOT_CODES = new Set([Codes.PROJECT_ROOT_INVALID, Codes.INVALID_ROOT]);

/**
 * @param {{
 *   gateway: import("../../domain/ports.js").ProjectGateway,
 *   navigate: (url: string) => void,
 * }} deps
 */
export const settingsPage = ({ gateway, navigate }) => () => ({
  /** The project as the server has it. @type {import("../../domain/project.js").Project} */
  project: { id: "", name: "", rootDir: "", ignoredPaths: [] },
  /** @type {import("../../domain/project.js").Repository[]} */
  repositories: [],
  name: "",
  rootDir: "",
  saving: false,
  nameError: null,
  rootError: null,
  /** "Saved." or what changed elsewhere. */
  notice: "",
  ignoredPath: "",
  ignoredError: null,
  addingPath: false,
  removingPath: "",
  confirmingDelete: false,
  typedName: "",
  deleting: false,
  /** Why the delete was refused: the sessions still running. */
  refusal: null,
  deletedElsewhere: false,
  error: null,
  ready: false,
  stopFeed: () => {},

  get changes() {
    return changedFields(this.project, { name: this.name, rootDir: this.rootDir });
  },
  get isDirty() {
    return Object.keys(this.changes).length > 0;
  },
  get rootProblem() {
    return this.changes.rootDir && !isAbsolutePath(this.rootDir) ? "Use an absolute path on the server's machine, like /src/app or ~/src/app." : "";
  },
  get cannotSave() {
    return !this.ready || this.saving || !this.isDirty || this.rootProblem !== "" || this.deletedElsewhere;
  },
  get canDelete() {
    return confirmsDelete(this.typedName, this.project.name) && !this.deleting && !this.deletedElsewhere;
  },
  /** The toolbar's crumb follows a rename. */
  get crumb() {
    return `${this.project.name} /`;
  },
  get nameInvalid() {
    return this.nameError !== null;
  },
  get nameDescribedBy() {
    return this.nameError ? "project-name-error" : null;
  },
  get rootInvalid() {
    return this.rootError !== null || this.rootProblem !== "";
  },
  get rootDescribedBy() {
    return this.rootError ? "project-root-error project-root-hint" : "project-root-hint";
  },
  get ignoredRows() {
    return this.project.ignoredPaths.map((path) => ({ path, removing: path === this.removingPath, removeLabel: `Remove ${path}` }));
  },
  get noIgnored() {
    return this.ready && this.project.ignoredPaths.length === 0;
  },
  get hasIgnored() {
    return this.ready && this.project.ignoredPaths.length > 0;
  },
  get ignoredInvalid() {
    return this.ignoredError !== null;
  },
  get ignoredDescribedBy() {
    return this.ignoredError ? "ignored-path-error" : null;
  },
  get cannotAddPath() {
    return !this.ready || this.addingPath || this.deletedElsewhere;
  },
  get cannotAskDelete() {
    return !this.ready || this.deletedElsewhere;
  },

  init() {
    try {
      const seed = gateway.decodeSettingsSeed(readSeed(this.$el));
      this.project = seed.project;
      this.repositories = seed.repositories;
      this.name = seed.project.name;
      this.rootDir = seed.project.rootDir;
    } catch (err) {
      this.error = describeError(err);
      return;
    }
    this.$nextTick(() => {
      for (const node of this.$el.querySelectorAll("[data-ssr]")) node.remove();
      this.ready = true;
    });
    this.stopFeed = gateway.followCatalog((change) => this.apply(change));
  },

  destroy() {
    this.stopFeed();
  },

  /** @param {import("../../domain/ports.js").CatalogChange} change */
  apply(change) {
    if (change.kind === "deleted") {
      if (change.id === this.project.id && !this.deleting) this.deletedElsewhere = true;
      return;
    }
    const next = change.project;
    if (next.id !== this.project.id) return;
    // Our own save answers for itself; its echo may come first.
    if (this.saving) {
      this.project = { ...this.project, ignoredPaths: next.ignoredPaths };
      return;
    }
    const wasDirty = this.isDirty;
    const moved = next.name !== this.project.name || next.rootDir !== this.project.rootDir;
    this.project = next;
    if (!wasDirty) {
      this.name = next.name;
      this.rootDir = next.rootDir;
    } else if (moved) {
      this.notice = `Changed elsewhere meanwhile: now “${next.name}” in ${next.rootDir}. Your edits are kept; saving replaces it.`;
    }
  },

  async save() {
    if (this.cannotSave) return;
    this.saving = true;
    this.nameError = this.rootError = this.error = null;
    this.notice = "";
    try {
      const saved = await gateway.updateProject({ projectId: this.project.id, ...this.changes });
      this.project = saved;
      this.name = saved.name;
      this.rootDir = saved.rootDir;
      this.notice = "Saved.";
    } catch (err) {
      const view = describeError(err);
      if (NAME_CODES.has(view.code)) this.nameError = view;
      else if (ROOT_CODES.has(view.code)) this.rootError = view;
      else this.error = view;
    } finally {
      this.saving = false;
    }
  },

  /** Esc: back to what is saved. */
  resetGeneral() {
    this.name = this.project.name;
    this.rootDir = this.project.rootDir;
    this.nameError = this.rootError = null;
    this.notice = "";
  },

  async addIgnored() {
    if (this.addingPath || this.deletedElsewhere) return;
    const problem = ignoredPathProblem(this.ignoredPath);
    if (problem) {
      this.ignoredError = { message: problem, code: "", next: "" };
      return;
    }
    this.addingPath = true;
    this.ignoredError = null;
    try {
      const saved = await gateway.addIgnoredPath(this.project.id, this.ignoredPath.trim());
      this.project = { ...this.project, ignoredPaths: saved.ignoredPaths };
      this.ignoredPath = "";
    } catch (err) {
      this.ignoredError = describeError(err);
    } finally {
      this.addingPath = false;
    }
  },

  async removeIgnored(path) {
    if (this.removingPath) return;
    this.removingPath = path;
    this.ignoredError = null;
    try {
      const saved = await gateway.removeIgnoredPath(this.project.id, path);
      this.project = { ...this.project, ignoredPaths: saved.ignoredPaths };
    } catch (err) {
      this.ignoredError = describeError(err);
    } finally {
      this.removingPath = "";
    }
  },

  askDelete() {
    this.confirmingDelete = true;
    this.typedName = "";
    this.refusal = null;
    this.$nextTick(() => this.$refs.confirmName?.focus());
  },
  cancelDelete() {
    this.confirmingDelete = false;
    this.typedName = "";
    this.refusal = null;
  },

  async deleteProject() {
    if (!this.canDelete) return;
    this.deleting = true;
    this.refusal = null;
    this.error = null;
    try {
      await gateway.deleteProject(this.project.id);
      navigate("/projects");
    } catch (err) {
      if (codeOf(err) === Codes.PROJECT_HAS_RUNNING_SESSIONS) {
        this.refusal = refusalView(this.project, err.details?.sessions ?? []);
      } else {
        this.error = describeError(err);
      }
    } finally {
      this.deleting = false;
    }
  },

  dismissError() {
    this.error = null;
  },
});

/**
 * The refusal in the page's own words (the server's message is written for
 * MCP clients): how many sessions, and each one linked to its task.
 * @param {import("../../domain/project.js").Project} project
 * @param {import("../../domain/project.js").RunningSession[]} sessions
 */
function refusalView(project, sessions) {
  const n = sessions.length;
  return {
    message: n
      ? `${project.name} still has ${n} running session${n === 1 ? "" : "s"}, so it was not deleted.`
      : `${project.name} still has running sessions, so it was not deleted.`,
    code: Codes.PROJECT_HAS_RUNNING_SESSIONS,
    next: describeError(new StructuredError(Codes.PROJECT_HAS_RUNNING_SESSIONS, "")).next,
    sessions: sessions.map((s) => ({
      id: s.id,
      label: s.agent || "a session",
      detail: ` · session ${s.id.slice(0, 8)}${s.ticketId ? "" : " · no task"}`,
      href: s.ticketId ? `/projects/${encodeURIComponent(project.id)}/tasks/${encodeURIComponent(s.ticketId)}` : "",
    })),
  };
}
