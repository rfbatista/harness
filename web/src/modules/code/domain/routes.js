// Every code view is a URL (UX §2.4), down to a file and a line:
//
//   /projects/{p}/tasks/{t}/sessions/{s}/code[/changes|/files/{path}|/map][?scope=&workspace=]
//   /projects/{p}/repositories/{r}/code[/files/{path}|/map][?ref=]
//
// The Go handlers parse the same paths (internal/adapter/in/web/code/routes.go).

/**
 * @typedef {"changes"|"files"|"map"} View
 *
 * @typedef {object} CodeRoute
 * @property {"session"|"repository"} kind
 * @property {string} projectId
 * @property {string} taskId        session routes
 * @property {string} sessionId     session routes
 * @property {string} repositoryId  repository routes
 * @property {View|""} view         "" is the default view
 * @property {string} path          the open file or directory under files, "" for none
 * @property {string} scope         a session diff's scope, "" for the default (all)
 * @property {string} ref           a repository's ref, "" for the checkout's branch
 * @property {string} workspace     a session's worktree when it has several, "" for the first
 * @property {string} commit        a permalink's commit hint on a session URL, "" for none
 */

const enc = encodeURIComponent;
const encPath = (path) => path.split("/").map(enc).join("/");

/** @param {Partial<CodeRoute>} r @returns {CodeRoute} */
export const route = (r) => ({
  kind: "repository",
  projectId: "",
  taskId: "",
  sessionId: "",
  repositoryId: "",
  view: "",
  path: "",
  scope: "",
  ref: "",
  workspace: "",
  commit: "",
  ...r,
});

/** The base of a route's code URLs. */
export function codeBase(r) {
  return r.kind === "session"
    ? `/projects/${enc(r.projectId)}/tasks/${enc(r.taskId)}/sessions/${enc(r.sessionId)}/code`
    : `/projects/${enc(r.projectId)}/repositories/${enc(r.repositoryId)}/code`;
}

/**
 * The URL of a route, with an optional hash ("#L12", "#diff-…").
 * @param {CodeRoute} r
 */
export function hrefOf(r, hash = "") {
  let path = codeBase(r);
  if (r.view === "files") path += r.path ? `/files/${encPath(r.path)}` : "/files";
  else if (r.view === "changes") path += "/changes";
  else if (r.view === "map") path += "/map";
  const q = new URLSearchParams();
  if (r.kind === "session") {
    if (r.view === "changes" && r.scope && r.scope !== "all") q.set("scope", r.scope);
    if (r.workspace) q.set("workspace", r.workspace);
    if (r.commit) q.set("commit", r.commit);
  } else if (r.ref) {
    q.set("ref", r.ref);
  }
  const qs = q.toString();
  return path + (qs ? `?${qs}` : "") + (hash && !hash.startsWith("#") ? `#${hash}` : hash);
}

/**
 * Reads a code URL; null when it is not one.
 * @param {string} pathname
 * @param {string} [search]
 * @returns {CodeRoute|null}
 */
export function parseHref(pathname, search = "") {
  const q = new URLSearchParams(search);
  const session = /^\/projects\/([^/]+)\/tasks\/([^/]+)\/sessions\/([^/]+)\/code(\/.*)?$/.exec(pathname);
  const repo = /^\/projects\/([^/]+)\/repositories\/([^/]+)\/code(\/.*)?$/.exec(pathname);
  const m = session ?? repo;
  if (!m) return null;
  const rest = (session ? session[4] : repo[3]) ?? "";
  const tail = parseTail(rest);
  if (!tail) return null;
  const dec = (s) => {
    try {
      return decodeURIComponent(s);
    } catch {
      return s;
    }
  };
  if (session) {
    return route({
      kind: "session",
      projectId: dec(m[1]),
      taskId: dec(m[2]),
      sessionId: dec(m[3]),
      ...tail,
      scope: tail.view === "changes" ? (q.get("scope") ?? "") : "",
      workspace: q.get("workspace") ?? "",
      commit: q.get("commit") ?? "",
    });
  }
  return route({ kind: "repository", projectId: dec(m[1]), repositoryId: dec(m[2]), ...tail, ref: q.get("ref") ?? "" });
}

function parseTail(rest) {
  if (rest === "" || rest === "/") return { view: "", path: "" };
  if (rest === "/changes") return { view: "changes", path: "" };
  if (rest === "/map") return { view: "map", path: "" };
  if (rest === "/files" || rest === "/files/") return { view: "files", path: "" };
  if (rest.startsWith("/files/")) {
    const path = rest
      .slice(7)
      .split("/")
      .filter(Boolean)
      .map((s) => {
        try {
          return decodeURIComponent(s);
        } catch {
          return s;
        }
      })
      .join("/");
    return { view: "files", path };
  }
  return null;
}

/**
 * The permalink Y copies: the same view pinned to a commit. On a repository
 * page ref becomes the commit; on a session the commit is a hint, since the
 * worktree's uncommitted lines can still change.
 */
export function permalink(r, commit, hash = "") {
  return r.kind === "session" ? hrefOf({ ...r, commit }, hash) : hrefOf({ ...r, ref: commit }, hash);
}

export const PERMALINK_TOAST = Object.freeze({
  repository: "Link copied.",
  session: "Link copied. It points at uncommitted work, which may change.",
});

/** The repository code page at a ref (the gone worktree's "Browse the branch"). */
export const repositoryHref = (projectId, repositoryId, ref = "", path = "") =>
  hrefOf(route({ kind: "repository", projectId, repositoryId, ref, view: path ? "files" : "", path }));

/** The task page's plain URL for a session (leaving the Code tab). */
export const sessionHref = (projectId, taskId, sessionId) => `/projects/${enc(projectId)}/tasks/${enc(taskId)}?session=${enc(sessionId)}`;
