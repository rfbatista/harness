// ProjectGateway in memory, for tests and web/dev pages. It obeys the same
// contract as the real gateway (../testing/gateway-contract.js).

import { Codes, StructuredError } from "../../../shared/domain/errors.js";
import { FeedStatus } from "../../../shared/domain/feed.js";
import { toEnvFilesSeed, toProjectsSeed, toRepositoriesSeed, toSettingsSeed } from "./dto.js";
import { cleanEnvPath, envPathProblem } from "../domain/project.js";

/** Session statuses that have ended; any other keeps a session live. */
const ENDED = new Set(["done", "failed", "stopped"]);

/**
 * @typedef {object} World
 * @property {object[]} [projects]      {id, name, rootDir, ignoredPaths?}
 * @property {object[]} [repositories]
 * @property {Record<string, import("../domain/project.js").FoundRepository[]>} [disk]  directory → the checkouts inside it
 * @property {Record<string, Record<string, string>>} [checkouts]  repository id → files in its checkout (path → content)
 * @property {string} [home]      what ~ expands to on the server's machine
 * @property {string[]} [dirs]    directories that exist there (disk's directories exist too)
 * @property {{ id: string, projectId: string, status: string, updatedAt: Date }[]} [tickets]
 * @property {{ id: string, projectId: string, ticketId: string, agent: string, status: string, lastActivityAt: Date|null }[]} [sessions]
 */

/** @param {World} [world] */
export function memoryProjects({
  projects = [],
  repositories = [],
  disk = {},
  checkouts = {},
  home = "/home/you",
  dirs = [],
  tickets = [],
  sessions = [],
} = {}) {
  /** repository id → path → env file */
  const envStore = new Map();
  const projectStore = new Map(projects.map((p) => [p.id, freeze({ ignoredPaths: [], ...p })]));
  const repoStore = new Map(repositories.map((r) => [r.id, r]));
  const sessionStore = new Map(sessions.map((s) => [s.id, { ...s }]));
  const existing = new Set([...dirs, ...Object.keys(disk)].map(clean));
  /** @type {Set<{ onChange: Function, onStatus: Function }>} */
  const followers = new Set();
  let next = 1;

  function announce(change) {
    for (const f of [...followers]) f.onChange(change);
  }

  function find(projectId) {
    const project = projectStore.get(projectId);
    if (!project) throw new StructuredError(Codes.PROJECT_NOT_FOUND, "project not found", 404);
    return project;
  }

  function save(project) {
    const frozen = freeze(project);
    projectStore.set(frozen.id, frozen);
    announce({ kind: "upsert", project: frozen });
    return frozen;
  }

  /** ~ expanded, cleaned; refused unless it is an existing absolute directory. */
  function resolveRoot(raw) {
    const root = clean(expand((raw ?? "").trim()));
    if (!root.startsWith("/") || !existing.has(root)) {
      throw new StructuredError(Codes.PROJECT_ROOT_INVALID, `${raw || "(empty)"} is not an existing directory on the server's machine`, 400);
    }
    return root;
  }

  function claimName(name, exceptId) {
    const taken = [...projectStore.values()].some((p) => p.id !== exceptId && p.name.toLowerCase() === name.toLowerCase());
    if (taken) throw new StructuredError(Codes.PROJECT_NAME_TAKEN, `another project is already called ${name}`, 409);
  }

  /** @type {import("../domain/ports.js").ProjectGateway} */
  const gateway = {
    decodeRepositories: toRepositoriesSeed,
    decodeEnvFiles: toEnvFilesSeed,
    decodeProjectsSeed: toProjectsSeed,
    decodeSettingsSeed: toSettingsSeed,

    async saveEnvFile(repositoryId, path, content) {
      if (!repoStore.has(repositoryId)) throw new StructuredError(Codes.REPOSITORY_NOT_FOUND, "repository not found", 404);
      if (envPathProblem(path)) throw new StructuredError(Codes.INVALID_PATH, "env file path must stay inside the repository", 400);
      if (content.length > 256 * 1024) throw new StructuredError(Codes.ENV_FILE_TOO_LARGE, "an env file holds at most 256 KB", 400);
      const cleanPath = cleanEnvPath(path);
      const file = Object.freeze({ path: cleanPath, content, updatedAt: new Date() });
      const files = envStore.get(repositoryId) ?? new Map();
      files.set(cleanPath, file);
      envStore.set(repositoryId, files);
      return file;
    },

    async deleteEnvFile(repositoryId, path) {
      if (!envStore.get(repositoryId)?.delete(path)) {
        throw new StructuredError(Codes.ENV_FILE_NOT_FOUND, `env file ${path} not found`, 404);
      }
    },

    async importEnvFile(repositoryId, path) {
      const content = checkouts[repositoryId]?.[path.trim()];
      if (content === undefined) throw new StructuredError(Codes.ENV_FILE_NOT_FOUND, `${path} does not exist in the checkout`, 404);
      return gateway.saveEnvFile(repositoryId, path, content);
    },

    async findRepositories(rootDir) {
      const root = rootDir.trim().replace(/\/+$/, "");
      if (!root.startsWith("/") || !(root in disk)) {
        throw new StructuredError(Codes.INVALID_ROOT, `${rootDir} is not a directory on the server's machine`, 400);
      }
      return { root, found: disk[root] };
    },

    async createProject({ name, rootDir }) {
      const trimmed = (name ?? "").trim();
      if (!trimmed) throw new StructuredError(Codes.INVALID_INPUT, "project name is required", 400);
      const root = resolveRoot(rootDir);
      claimName(trimmed, "");
      return save({ id: `proj-${next++}`, name: trimmed, rootDir: root, ignoredPaths: [] });
    },

    async updateProject({ projectId, name, rootDir }) {
      if (!projectId) throw new StructuredError(Codes.INVALID_INPUT, "project_id is required", 400);
      const project = find(projectId);
      const changes = {};
      const newName = (name ?? "").trim();
      if (newName && newName !== project.name) {
        if (newName.toLowerCase() !== project.name.toLowerCase()) claimName(newName, projectId);
        changes.name = newName;
      }
      if ((rootDir ?? "").trim() && clean(expand(rootDir.trim())) !== project.rootDir) {
        changes.rootDir = resolveRoot(rootDir);
      }
      return save({ ...project, ...changes });
    },

    async deleteProject(projectId) {
      find(projectId);
      const live = [...sessionStore.values()].filter((s) => s.projectId === projectId && !ENDED.has(s.status));
      if (live.length > 0) {
        throw new StructuredError(
          Codes.PROJECT_HAS_RUNNING_SESSIONS,
          `the project has ${live.length} running session${live.length === 1 ? "" : "s"}; stop them first`,
          409,
          { sessions: live.map((s) => ({ id: s.id, ticketId: s.ticketId ?? "", agent: s.agent ?? "" })) },
        );
      }
      projectStore.delete(projectId);
      for (const r of [...repoStore.values()]) if (r.projectId === projectId) repoStore.delete(r.id);
      announce({ kind: "deleted", id: projectId });
    },

    async addIgnoredPath(projectId, path) {
      const project = find(projectId);
      const p = normalizeIgnored(path);
      if (!p) throw new StructuredError(Codes.INVALID_PATH, "path is required", 400);
      if (project.ignoredPaths.includes(p)) return project;
      return save({ ...project, ignoredPaths: [...project.ignoredPaths, p] });
    },

    async removeIgnoredPath(projectId, path) {
      const project = find(projectId);
      const p = normalizeIgnored(path);
      return save({ ...project, ignoredPaths: project.ignoredPaths.filter((x) => x !== p) });
    },

    async listProjectSummaries() {
      return [...projectStore.values()]
        .map((project) => {
          const ownTickets = tickets.filter((t) => t.projectId === project.id);
          const ownSessions = [...sessionStore.values()].filter((s) => s.projectId === project.id);
          const times = [...ownTickets.map((t) => t.updatedAt), ...ownSessions.map((s) => s.lastActivityAt)].filter(Boolean);
          return Object.freeze({
            project,
            repositoryCount: [...repoStore.values()].filter((r) => r.projectId === project.id).length,
            openTaskCount: ownTickets.filter((t) => t.status !== "done").length,
            runningSessionCount: ownSessions.filter((s) => !ENDED.has(s.status)).length,
            lastActivityAt: times.length ? new Date(Math.max(...times.map((t) => t.getTime()))) : null,
          });
        })
        .sort((a, b) => {
          const x = a.project.name.toLowerCase();
          const y = b.project.name.toLowerCase();
          return x !== y ? (x < y ? -1 : 1) : a.project.id < b.project.id ? -1 : 1;
        });
    },

    followCatalog(onChange, onStatus = () => {}) {
      const follower = { onChange, onStatus };
      followers.add(follower);
      queueMicrotask(() => followers.has(follower) && onStatus(FeedStatus.LIVE));
      return () => followers.delete(follower);
    },

    async addRepository({ projectId, name, description, url, rootDir }) {
      if (!projectStore.has(projectId)) throw new StructuredError(Codes.PROJECT_NOT_FOUND, "project not found", 404);
      if (!url?.trim()) throw new StructuredError(Codes.INVALID_URL, "repository url is required", 400);
      const repo = Object.freeze({
        id: `repo-${next++}`,
        projectId,
        name: name ?? "",
        description: description ?? "",
        url: url.trim(),
        rootDir: rootDir ?? "",
      });
      repoStore.set(repo.id, repo);
      return repo;
    },

    async removeRepository(repositoryId) {
      if (!repoStore.delete(repositoryId)) {
        throw new StructuredError(Codes.REPOSITORY_NOT_FOUND, "repository not found", 404);
      }
    },
  };

  function expand(path) {
    return path === "~" ? home : path.startsWith("~/") ? `${home}/${path.slice(2)}` : path;
  }

  return {
    gateway,
    projects: () => [...projectStore.values()],
    repositories: (projectId) => [...repoStore.values()].filter((r) => r.projectId === projectId),
    envFiles: (repositoryId) => [...(envStore.get(repositoryId)?.values() ?? [])],
    /** The server's side: a session of a project starts, or ends. */
    startSession: (session) => sessionStore.set(session.id, { ticketId: "", agent: "", lastActivityAt: new Date(), ...session }),
    endSession: (sessionId) => {
      const s = sessionStore.get(sessionId);
      if (s) s.status = "done";
    },
    /** The feed's side: the stream drops, or comes back having missed changes. */
    reportStatus: (status) => {
      for (const f of [...followers]) f.onStatus(status);
    },
  };
}

const freeze = (project) => Object.freeze({ ...project, ignoredPaths: Object.freeze([...(project.ignoredPaths ?? [])]) });

/** An absolute path without ., .., doubled or trailing slashes (like Go's filepath.Clean). */
function clean(path) {
  if (!path.startsWith("/")) return path.replace(/\/+$/, "");
  const parts = [];
  for (const part of path.split("/")) {
    if (part === "" || part === ".") continue;
    if (part === "..") parts.pop();
    else parts.push(part);
  }
  return `/${parts.join("/")}`;
}

/** An ignored path as the server stores it (domain.NormalizePath): cleaned, no leading slash. */
function normalizeIgnored(path) {
  return clean(`/${path.trim().replace(/\\/g, "/")}`).slice(1);
}
