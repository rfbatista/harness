// TokenSaverGateway over the harness HTTP API.

import { toActivations } from "./dto.js";

/**
 * @param {import("../../../shared/infrastructure/api.js").ApiClient} api
 * @returns {import("../domain/ports.js").TokenSaverGateway}
 */
export function tokenSaversGateway(api) {
  return {
    decodeSeed: toActivations,
    async set(level, ownerId, saver, value) {
      return toActivations(await api.post("/set_token_saver_activation", { level, owner_id: ownerId, saver, value }));
    },
  };
}
