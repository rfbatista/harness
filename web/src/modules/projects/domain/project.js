// Projects and their repositories: a project is a codebase the harness works
// on, usually a directory holding many git checkouts; each repository is one
// of them, where sessions cut their worktrees. Mirrors internal/domain/project.go and repository.go.

/**
 * @typedef {object} Project
 * @property {string} id
 * @property {string} name
 * @property {string} rootDir
 * @property {string[]} ignoredPaths  hidden from the project's file tree, relative to rootDir
 *
 * @typedef {object} ProjectSummary  a project at a glance, as the projects list shows it
 * @property {Project} project
 * @property {number} repositoryCount
 * @property {number} openTaskCount        tasks not done
 * @property {number} runningSessionCount  sessions not done, failed or stopped
 * @property {Date|null} lastActivityAt    newest task update or session activity
 *
 * @typedef {object} RunningSession  one session that keeps a project from being deleted
 * @property {string} id
 * @property {string} ticketId  "" for a session without a task
 * @property {string} agent     "" when it has none
 *
 * @typedef {object} Repository
 * @property {string} id
 * @property {string} projectId
 * @property {string} name
 * @property {string} description
 * @property {string} url       its git remote; file://<path> for a local-only one
 * @property {string} rootDir   the checkout sessions cut worktrees from
 *
 * @typedef {object} EnvFile  a file of variables git does not carry, written into every new session worktree
 * @property {string} path       relative to the repository's checkout
 * @property {string} content
 * @property {Date|null} updatedAt
 *
 * @typedef {object} FoundRepository  a git checkout found on disk, not necessarily added
 * @property {string} path    its absolute directory
 * @property {string} name
 * @property {string} remote  its origin's URL; "" for a local-only checkout
 */

/** The last segment of a path: what a project or repository is called when not named. */
export function baseName(path) {
  const parts = path.trim().replace(/[\\/]+$/, "").split(/[\\/]/);
  return parts.at(-1) || "";
}

/** A path is usable when it is absolute (/…, ~ or ~/… on the server's machine, which expands ~). */
export const isAbsolutePath = (path) => /^(\/|~\/|~$)/.test(path.trim());

/** Orders summaries as the server does: by name ignoring case, then by id. */
export function byName(a, b) {
  const x = a.project.name.toLowerCase();
  const y = b.project.name.toLowerCase();
  if (x !== y) return x < y ? -1 : 1;
  return a.project.id < b.project.id ? -1 : a.project.id > b.project.id ? 1 : 0;
}

/** Whether a summary's project matches the list's filter, by name or directory. */
export function matchesQuery(summary, query) {
  const q = query.trim().toLowerCase();
  if (!q) return true;
  const { name, rootDir } = summary.project;
  return name.toLowerCase().includes(q) || rootDir.toLowerCase().includes(q);
}

/** @param {ProjectSummary[]} summaries */
export const sessionsRunning = (summaries) => summaries.reduce((n, s) => n + s.runningSessionCount, 0);

/** A delete goes ahead only once the project's name is typed exactly. */
export function confirmsDelete(typed, name) {
  return name !== "" && typed.trim() === name;
}

/**
 * Why an ignored path is refused, or "" when it is fine: relative to the
 * project's directory and naming something inside it.
 */
export function ignoredPathProblem(path) {
  const p = path.trim().replace(/\\/g, "/").replace(/\/+$/, "");
  if (!p) return "Give a path inside the project, like node_modules or web/vendor.";
  if (p === "." || p.split("/").includes("..")) return "The path must name something inside the project.";
  return "";
}

/**
 * The fields of a settings form that differ from the saved project, trimmed:
 * what update_project should be sent. An empty field keeps the saved value,
 * as on the server, so it is never sent.
 * @param {Project} saved
 * @param {{ name: string, rootDir: string }} form
 * @returns {{ name?: string, rootDir?: string }}
 */
export function changedFields(saved, form) {
  const changes = {};
  const name = form.name.trim();
  const rootDir = form.rootDir.trim();
  if (name && name !== saved.name) changes.name = name;
  if (rootDir && rootDir !== saved.rootDir) changes.rootDir = rootDir;
  return changes;
}

/** Found checkouts not yet added to the project (matched by path). */
export function notYetAdded(found, repositories) {
  const added = new Set(repositories.map((r) => trimSlash(r.rootDir)));
  return found.filter((f) => !added.has(trimSlash(f.path)));
}

/** A found checkout's path, shown relative to the directory it was found in. */
export function relativeTo(root, path) {
  const base = trimSlash(root);
  const p = trimSlash(path);
  if (p === base) return ".";
  return p.startsWith(`${base}/`) ? p.slice(base.length + 1) : p;
}

const trimSlash = (path) => path.trim().replace(/\/+$/, "");

/**
 * Why an env file path is refused, or "" when it is fine: it must stay inside
 * the checkout (relative, never above it) and outside .git. Mirrors
 * domain.CleanEnvFilePath on the server.
 */
export function envPathProblem(path) {
  const p = path.trim().replace(/\\/g, "/");
  if (!p) return "Give the file's path in the repository, like .env or apps/api/.env.";
  if (p.startsWith("/") || p.startsWith("~")) return "Use a path relative to the repository, like .env.";
  const resolved = [];
  for (const part of p.split("/")) {
    if (part === "" || part === ".") continue;
    if (part === "..") {
      if (resolved.length === 0) return "The path must stay inside the repository.";
      resolved.pop();
    } else {
      resolved.push(part);
    }
  }
  if (resolved.length === 0) return "The path must name a file inside the repository.";
  if (resolved[0] === ".git") return "The path must not be inside .git.";
  return "";
}

/** An env file path as the server stores it: resolved, no ./, forward slashes. Assumes it is valid. */
export function cleanEnvPath(path) {
  const resolved = [];
  for (const part of path.trim().replace(/\\/g, "/").split("/")) {
    if (part === "" || part === ".") continue;
    if (part === "..") resolved.pop();
    else resolved.push(part);
  }
  return resolved.join("/");
}

/** A repository without a remote is recorded by its path. */
export function repositoryURL(url, rootDir) {
  return url.trim() || `file://${rootDir.trim()}`;
}
