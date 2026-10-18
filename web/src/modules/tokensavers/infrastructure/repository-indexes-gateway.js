// RepositoryIndexGateway over the harness HTTP API.

import { toRepositoryIndex, toRepositoryIndexes } from "./repository-indexes-dto.js";

/**
 * @param {import("../../../shared/infrastructure/api.js").ApiClient} api
 * @returns {import("../domain/repository-index-ports.js").RepositoryIndexGateway}
 */
export function repositoryIndexesGateway(api) {
  return {
    decodeSeed: toRepositoryIndexes,
    async list() {
      return toRepositoryIndexes(await api.get("/list_repository_indexes"));
    },
    async rebuild(repositoryId) {
      const body = await api.post("/rebuild_repository_index", { repository_id: repositoryId });
      return toRepositoryIndex(body?.repository_index);
    },
  };
}
