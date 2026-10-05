// ProjectGateway over the harness HTTP API (RPC-style POSTs).

import { toEnvFile, toEnvFilesSeed, toFound, toProject, toRepositoriesSeed, toRepository } from "./dto.js";

/**
 * @param {import("../../../shared/infrastructure/api.js").ApiClient} api
 * @returns {import("../domain/ports.js").ProjectGateway}
 */
export function projectsGateway(api) {
  return {
    decodeRepositories: toRepositoriesSeed,
    decodeEnvFiles: toEnvFilesSeed,

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
