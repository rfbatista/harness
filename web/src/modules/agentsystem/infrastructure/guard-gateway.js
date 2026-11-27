// A repository's Guard line over the harness HTTP API: the same read the Agent
// system page makes, summarised (domain/guardLine.js).

import { guardLine } from "../domain/guardLine.js";

/** @param {import("../../../shared/infrastructure/api.js").ApiClient} api */
export function guardGateway(api) {
  return {
    /**
     * @param {string} projectId
     * @param {string} repositoryId
     * @returns {Promise<import("../domain/guardLine.js").GuardLine>}
     */
    async line(projectId, repositoryId) {
      const path = `/projects/${encodeURIComponent(projectId)}/repositories/${encodeURIComponent(repositoryId)}/agent-system`;
      return guardLine(await api.get(path));
    },
  };
}
