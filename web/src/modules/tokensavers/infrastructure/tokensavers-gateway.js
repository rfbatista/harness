// TokenSaverGateway over the harness HTTP API. The caveman intensity is a
// global setting (domain.SettingCavemanIntensity).

import { Codes, StructuredError } from "../../../shared/domain/errors.js";
import { toActivations, toRepositoryIndex, toRepositoryIndexes, toSaver, toSaverList } from "./dto.js";

const INTENSITY_KEY = "token_savers.caveman.intensity";

/**
 * @param {import("../../../shared/infrastructure/api.js").ApiClient} api
 * @returns {import("../domain/ports.js").TokenSaverGateway}
 */
export function tokenSaversGateway(api) {
  return {
    decodeActivations: toActivations,
    async setActivation(level, ownerId, saver, value) {
      return toActivations(await api.post("/set_token_saver_activation", { level, owner_id: ownerId, saver, value }));
    },

    decodeSavers: toSaverList,
    async listSavers() {
      return toSaverList(await api.get("/list_token_savers"));
    },
    async installSaver(saver) {
      const body = await api.post("/install_token_saver", { saver });
      return toSaver(body?.token_saver);
    },
    async removeSaver(saver) {
      const body = await api.post("/remove_token_saver", { saver });
      return toSaver(body?.token_saver);
    },

    decodeIndexes: toRepositoryIndexes,
    async listIndexes() {
      return toRepositoryIndexes(await api.get("/list_repository_indexes"));
    },
    async rebuildIndex(repositoryId) {
      const body = await api.post("/rebuild_repository_index", { repository_id: repositoryId });
      return toRepositoryIndex(body?.repository_index);
    },

    async setCavemanIntensity(intensity) {
      const body = await api.post("/update_settings", { settings: { [INTENSITY_KEY]: intensity } });
      const stored = body?.settings?.[INTENSITY_KEY];
      if (typeof stored !== "string") {
        throw new StructuredError(Codes.BAD_RESPONSE, "Unexpected settings data: no caveman intensity.");
      }
      return /** @type {import("../domain/ports.js").CavemanIntensity} */ (stored);
    },
  };
}
