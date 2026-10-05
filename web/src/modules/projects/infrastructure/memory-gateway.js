// ProjectGateway in memory, for tests and web/dev pages. It obeys the same
// contract as the real gateway (../testing/gateway-contract.js).

import { Codes, StructuredError } from "../../../shared/domain/errors.js";
import { toEnvFilesSeed, toRepositoriesSeed } from "./dto.js";
import { cleanEnvPath, envPathProblem } from "../domain/project.js";

/**
 * @param {{
 *   projects?: object[],
 *   repositories?: object[],
 *   disk?: Record<string, import("../domain/project.js").FoundRepository[]>,  directory → the checkouts inside it
 *   checkouts?: Record<string, Record<string, string>>,  repository id → files in its checkout (path → content)
 * }} [world]
 */
export function memoryProjects({ projects = [], repositories = [], disk = {}, checkouts = {} } = {}) {
  /** repository id → path → env file */
  const envStore = new Map();
  const projectStore = new Map(projects.map((p) => [p.id, p]));
  const repoStore = new Map(repositories.map((r) => [r.id, r]));
  let next = 1;

  /** @type {import("../domain/ports.js").ProjectGateway} */
  const gateway = {
    decodeRepositories: toRepositoriesSeed,
    decodeEnvFiles: toEnvFilesSeed,

    async saveEnvFile(repositoryId, path, content) {
      if (!repoStore.has(repositoryId)) throw new StructuredError(Codes.REPOSITORY_NOT_FOUND, "repository not found", 404);
      if (envPathProblem(path)) throw new StructuredError(Codes.INVALID_PATH, "env file path must stay inside the repository", 400);
      if (content.length > 256 * 1024) throw new StructuredError(Codes.ENV_FILE_TOO_LARGE, "an env file holds at most 256 KB", 400);
      const clean = cleanEnvPath(path);
      const file = Object.freeze({ path: clean, content, updatedAt: new Date() });
      const files = envStore.get(repositoryId) ?? new Map();
      files.set(clean, file);
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
      if (!rootDir?.trim()) throw new StructuredError(Codes.INVALID_ROOT, "project root directory is required", 400);
      const project = Object.freeze({ id: `proj-${next++}`, name: name ?? "", rootDir: rootDir.trim() });
      projectStore.set(project.id, project);
      return project;
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

  return {
    gateway,
    projects: () => [...projectStore.values()],
    repositories: (projectId) => [...repoStore.values()].filter((r) => r.projectId === projectId),
    envFiles: (repositoryId) => [...(envStore.get(repositoryId)?.values() ?? [])],
  };
}
