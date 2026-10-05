// The new-project form. A project is a directory that usually holds many git
// checkouts; once the directory is given, the form asks the server which
// repositories are inside and offers them, all ticked. Creating the project
// adds each ticked one, then opens the project.
//
//   <form x-data="projectsNewProject" x-on:submit.prevent="submit">

import { describeError } from "../../../../shared/presentation/errors.js";
import { baseName, isAbsolutePath, relativeTo, repositoryURL } from "../../domain/project.js";

/**
 * @param {{
 *   gateway: import("../../domain/ports.js").ProjectGateway,
 *   navigate: (url: string) => void,
 * }} deps
 */
export const newProject = ({ gateway, navigate }) => () => ({
  rootDir: "",
  name: "",
  /** The directory as the server resolved it (~ expanded) after a search. */
  resolvedRoot: "",
  /** @type {{ path: string, name: string, remote: string, relative: string, remoteLabel: string, selected: boolean }[]} */
  found: [],
  searchedFor: "",
  searching: false,
  submitting: false,
  error: null,
  /** Set once the project exists, so a retry adds what is missing instead of creating it again. */
  createdProjectId: "",
  /** Paths already added to the created project. */
  added: [],

  get namePlaceholder() {
    return baseName(this.rootDir) || "named after its directory";
  },
  get pathProblem() {
    return this.rootDir.trim() && !isAbsolutePath(this.rootDir) ? "Use an absolute path, like /Users/you/src/app or ~/src/app." : "";
  },
  get hasFound() {
    return this.found.length > 0;
  },
  get searched() {
    return this.searchedFor !== "" && this.searchedFor === this.rootDir.trim();
  },
  get foundNothing() {
    return this.searched && this.found.length === 0;
  },
  get selectedCount() {
    return this.found.filter((f) => f.selected).length;
  },
  get submitLabel() {
    const n = this.selectedCount;
    return n === 0 ? "Create project" : `Create project with ${n} ${n === 1 ? "repository" : "repositories"}`;
  },
  get cannotSearch() {
    return this.searching || !this.rootDir.trim() || this.pathProblem !== "";
  },
  get cannotSubmit() {
    return this.submitting || this.searching || !this.rootDir.trim() || this.pathProblem !== "";
  },
  get createdProjectHref() {
    return this.createdProjectId ? `/projects/${encodeURIComponent(this.createdProjectId)}/repositories` : "";
  },

  /** Looks for the repositories inside the directory (on change, or the button). */
  async search() {
    if (this.cannotSearch || this.searched) return;
    const dir = this.rootDir.trim();
    this.searching = true;
    this.error = null;
    try {
      const { root, found } = await gateway.findRepositories(dir);
      this.resolvedRoot = root;
      this.found = found.map((f) => ({
        ...f,
        relative: relativeTo(root, f.path),
        remoteLabel: f.remote || "local only",
        selected: true,
      }));
      this.searchedFor = dir;
    } catch (err) {
      this.found = [];
      this.resolvedRoot = "";
      this.error = describeError(err);
    } finally {
      this.searching = false;
    }
  },

  async submit() {
    if (this.cannotSubmit) return;
    if (!this.searched) await this.search();
    if (this.error && !this.createdProjectId) return; // the directory itself is wrong
    this.submitting = true;
    this.error = null;
    const rootDir = this.resolvedRoot || this.rootDir.trim();
    try {
      if (!this.createdProjectId) {
        const project = await gateway.createProject({ name: this.name.trim() || baseName(rootDir), rootDir });
        this.createdProjectId = project.id;
      }
      for (const f of this.found.filter((x) => x.selected && !this.added.includes(x.path))) {
        await gateway.addRepository({
          projectId: this.createdProjectId,
          name: f.name || baseName(f.path),
          description: "",
          url: repositoryURL(f.remote, f.path),
          rootDir: f.path,
        });
        this.added = [...this.added, f.path];
      }
      navigate(`/projects/${encodeURIComponent(this.createdProjectId)}`);
    } catch (err) {
      this.error = describeError(err);
    } finally {
      this.submitting = false;
    }
  },
});
