// A project's repositories: the git checkouts its sessions cut worktrees
// from. Lists them; offers the checkouts found inside the project's
// directory that are not added yet; adds any other by path; removes one
// after an inline confirmation.
//
//   <main x-data="projectsRepositoriesPage" data-seed="repositories-seed">

import { describeError } from "../../../../shared/presentation/errors.js";
import { readSeed } from "../../../../shared/presentation/seed.js";
import { baseName, isAbsolutePath, notYetAdded, relativeTo, repositoryURL } from "../../domain/project.js";

/**
 * @param {{
 *   gateway: import("../../domain/ports.js").ProjectGateway,
 *   architecture?: { has: (repositoryId: string) => Promise<boolean> },
 *   designSystem?: { setRepository: (repositoryId: string, uses: boolean, path: string) => Promise<unknown> },
 * }} deps  architecture says whether a repository has a saved architecture, for its link's words;
 *          designSystem answers the question asked after adding a repository to a project that has one.
 */
export const repositoriesPage = ({ gateway, architecture, designSystem }) => () => ({
  projectId: "",
  projectRoot: "",
  /** Checkouts found inside the project's directory. @type {import("../../domain/project.js").FoundRepository[]} */
  found: [],
  searching: false,
  addingPath: "",
  /** @type {import("../../domain/project.js").Repository[]} */
  repositories: [],
  rootDir: "",
  name: "",
  remoteUrl: "",
  adding: false,
  removingId: "",
  confirmingId: "",
  /** Repository ids known to have an architecture. @type {string[]} */
  withArchitecture: [],
  /** Repositories just added to a project with a design system, still to be asked about it. @type {{ id: string, name: string }[]} */
  questions: [],
  error: null,
  ready: false,

  get rows() {
    return this.repositories.map((r) => ({
      id: r.id,
      name: r.name || baseName(r.rootDir) || r.url,
      rootDir: r.rootDir || "no local path: sessions cannot run here",
      remote: r.url.startsWith("file://") ? "local only" : r.url,
      confirming: r.id === this.confirmingId,
      removing: r.id === this.removingId,
      envHref: `/projects/${encodeURIComponent(this.projectId)}/repositories/${encodeURIComponent(r.id)}/env`,
      historyHref: `/projects/${encodeURIComponent(this.projectId)}/repositories/${encodeURIComponent(r.id)}/history`,
      architectureHref: `/projects/${encodeURIComponent(this.projectId)}/repositories/${encodeURIComponent(r.id)}/architecture`,
      architectureLabel: this.withArchitecture.includes(r.id) ? "Architecture" : "Set up architecture",
    }));
  },
  /** Found checkouts not added yet, to add with one click. */
  get suggestions() {
    return notYetAdded(this.found, this.repositories).map((f) => ({
      ...f,
      relative: relativeTo(this.projectRoot, f.path),
      remoteLabel: f.remote || "local only",
      adding: f.path === this.addingPath,
    }));
  },
  get hasSuggestions() {
    return this.suggestions.length > 0;
  },
  get isAsking() {
    return this.questions.length > 0;
  },
  get isEmpty() {
    return this.repositories.length === 0;
  },
  get namePlaceholder() {
    return baseName(this.rootDir) || "named after its directory";
  },
  get pathProblem() {
    return this.rootDir.trim() && !isAbsolutePath(this.rootDir) ? "Use an absolute path on the server's machine." : "";
  },
  get cannotAdd() {
    return !this.ready || this.adding || !this.rootDir.trim() || this.pathProblem !== "";
  },

  init() {
    try {
      const seed = gateway.decodeRepositories(readSeed(this.$el));
      this.projectId = seed.projectId;
      this.projectRoot = seed.projectRoot;
      this.repositories = seed.repositories;
    } catch (err) {
      this.error = describeError(err);
      return;
    }
    this.$nextTick(() => {
      for (const node of this.$el.querySelectorAll("[data-ssr]")) node.remove();
      this.ready = true;
    });
    this.searchProject();
    this.findArchitectures();
  },

  /** Which repositories have an architecture; a failure leaves the link saying Set up. */
  async findArchitectures() {
    if (!architecture) return;
    const asked = await Promise.allSettled(this.repositories.map((r) => architecture.has(r.id)));
    // a failure keeps the link's first words
    this.withArchitecture = this.repositories.filter((r, i) => asked[i].status === "fulfilled" && asked[i].value).map((r) => r.id);
  },

  /** Finds the checkouts inside the project's directory; a failure only means no suggestions. */
  async searchProject() {
    if (!this.projectRoot) return;
    this.searching = true;
    try {
      this.found = (await gateway.findRepositories(this.projectRoot)).found;
    } catch {
      this.found = [];
    } finally {
      this.searching = false;
    }
  },

  async addFound(path) {
    const f = this.found.find((x) => x.path === path);
    if (!f || this.addingPath) return;
    this.addingPath = path;
    this.error = null;
    try {
      const repo = await gateway.addRepository({
        projectId: this.projectId,
        name: f.name || baseName(f.path),
        description: "",
        url: repositoryURL(f.remote, f.path),
        rootDir: f.path,
      });
      this.added(repo);
    } catch (err) {
      this.error = describeError(err);
    } finally {
      this.addingPath = "";
    }
  },

  async add() {
    if (this.cannotAdd) return;
    this.adding = true;
    this.error = null;
    const rootDir = this.rootDir.trim();
    try {
      const repo = await gateway.addRepository({
        projectId: this.projectId,
        name: this.name.trim() || baseName(rootDir),
        description: "",
        url: repositoryURL(this.remoteUrl, rootDir),
        rootDir,
      });
      this.added(repo);
      this.rootDir = "";
      this.name = "";
      this.remoteUrl = "";
    } catch (err) {
      this.error = describeError(err);
    } finally {
      this.adding = false;
    }
  },

  /** Lists a repository just added, and queues the design system question when the server asked for it. */
  added(repo) {
    this.repositories = [...this.repositories, repo];
    if (repo.askDesignSystem && designSystem) this.questions = [...this.questions, { id: repo.id, name: repo.name || baseName(repo.rootDir) || repo.url }];
  },

  /** Yes opts the repository in at the default path; No leaves it off. The question is not asked again either way. */
  async answerDesignSystem(id, uses) {
    this.error = null;
    try {
      await designSystem.setRepository(id, uses, "");
      this.questions = this.questions.filter((q) => q.id !== id);
    } catch (err) {
      this.error = describeError(err);
    }
  },

  askRemove(id) {
    this.confirmingId = id;
  },
  cancelRemove() {
    this.confirmingId = "";
  },

  async remove(id) {
    if (this.removingId) return;
    this.removingId = id;
    this.error = null;
    try {
      await gateway.removeRepository(id);
      this.repositories = this.repositories.filter((r) => r.id !== id);
      this.confirmingId = "";
    } catch (err) {
      this.error = describeError(err);
    } finally {
      this.removingId = "";
    }
  },

  dismissError() {
    this.error = null;
  },
});
