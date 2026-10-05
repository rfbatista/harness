// Projects and their repositories: a project is a codebase the harness works
// on, usually a directory holding many git checkouts; each repository is one
// of them, where sessions cut their worktrees. Mirrors internal/domain/project.go and repository.go.

/**
 * @typedef {object} Project
 * @property {string} id
 * @property {string} name
 * @property {string} rootDir
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

/** A path is usable when it is absolute (/… or ~/… on the server's machine). */
export const isAbsolutePath = (path) => /^(\/|~\/)/.test(path.trim());

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
