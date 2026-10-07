// ProjectGateway over the harness HTTP API (RPC-style POSTs) and the project
// catalog feed (/api/project_events, server-sent events).

import { Codes, StructuredError } from "../../../shared/domain/errors.js";
import {
  toCatalogChange,
  toEnvFile,
  toEnvFilesSeed,
  toFound,
  toProject,
  toProjectsSeed,
  toRepositoriesSeed,
  toRepository,
  toRunningSessions,
  toSettingsSeed,
} from "./dto.js";

/**
 * @param {import("../../../shared/infrastructure/api.js").ApiClient} api
 * @param {import("../../../shared/infrastructure/feed.js").Feed} events
 * @returns {import("../domain/ports.js").ProjectGateway}
 */
export function projectsGateway(api, events) {
  return {
    decodeRepositories: toRepositoriesSeed,
    decodeEnvFiles: toEnvFilesSeed,
    decodeProjectsSeed: toProjectsSeed,
    decodeSettingsSeed: toSettingsSeed,

    async listProjectSummaries() {
      return toProjectsSeed(await api.get("/list_project_summaries"));
    },

    async updateProject({ projectId, name, rootDir }) {
      const body = await api.post("/update_project", { project_id: projectId, name, root_dir: rootDir });
      return toProject(body?.project);
    },

    async deleteProject(projectId) {
      try {
        await api.post("/delete_project", { project_id: projectId });
      } catch (err) {
        if (err instanceof StructuredError && err.code === Codes.PROJECT_HAS_RUNNING_SESSIONS) {
          throw new StructuredError(err.code, err.message, err.status, { sessions: toRunningSessions(err.details) });
        }
        throw err;
      }
    },

    async addIgnoredPath(projectId, path) {
      const body = await api.post("/add_ignored_path", { project_id: projectId, path });
      return toProject(body?.project);
    },

    async removeIgnoredPath(projectId, path) {
      const body = await api.post("/remove_ignored_path", { project_id: projectId, path });
      return toProject(body?.project);
    },

    followCatalog(onChange, onStatus = () => {}) {
      return events.follow("/project_events", {
        onMessage(data) {
          let change;
          try {
            change = toCatalogChange(data);
          } catch {
            return; // a malformed project is skipped, the stream goes on
          }
          if (change) onChange(change);
        },
        onStatus,
      });
    },

    async saveEnvFile(repositoryId, path, content) {
      const body = await api.post("/save_repository_env_file", { repository_id: repositoryId, path, content });
      return toEnvFile(body?.env_file);
    },

    async deleteEnvFile(repositoryId, path) {
      await api.post("/delete_repository_env_file", { repository_id: repositoryId, path });
    },

    async importEnvFile(repositoryId, path) {
      const body = await api.post("/import_repository_env_file", { repository_id: repositoryId, path });
      return toEnvFile(body?.env_file);
    },

    async findRepositories(rootDir) {
      return toFound(await api.get("/find_repositories", { root_dir: rootDir }));
    },

    async createProject({ name, rootDir }) {
      const body = await api.post("/create_project", { name, root_dir: rootDir });
      return toProject(body?.project);
    },

    async addRepository({ projectId, name, description, url, rootDir }) {
      const body = await api.post("/create_repository", {
        project_id: projectId,
        name,
        description,
        url,
        root_dir: rootDir,
      });
      return toRepository(body?.repository);
    },

    async removeRepository(repositoryId) {
      await api.post("/delete_repository", { repository_id: repositoryId });
    },
  };
}
