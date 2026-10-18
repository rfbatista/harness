// TokenSaverInstallGateway over the harness HTTP API.

import { toSaver, toSaverList } from "./installations-dto.js";

/**
 * @param {import("../../../shared/infrastructure/api.js").ApiClient} api
 * @returns {import("../domain/installation-ports.js").TokenSaverInstallGateway}
 */
export function tokenSaverInstallsGateway(api) {
  return {
    decodeSeed: toSaverList,
    async list() {
      return toSaverList(await api.get("/list_token_savers"));
    },
    async install(saver) {
      const body = await api.post("/install_token_saver", { saver });
      return toSaver(body?.token_saver);
    },
    async remove(saver) {
      const body = await api.post("/remove_token_saver", { saver });
      return toSaver(body?.token_saver);
    },
  };
}
