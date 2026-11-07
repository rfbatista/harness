// ServicesGateway over the harness HTTP API.

import { Codes, StructuredError } from "../../../shared/domain/errors.js";

/** @returns {import("../domain/ports.js").ServicesSettings} */
export function toSettings(body) {
  const runtime = body?.container_runtime;
  if ((runtime !== "docker" && runtime !== "podman") || typeof body?.available !== "boolean") {
    throw new StructuredError(Codes.BAD_RESPONSE, "Unexpected services settings: expected {container_runtime, available}.");
  }
  return { container_runtime: runtime, available: body.available };
}

/**
 * @param {import("../../../shared/infrastructure/api.js").ApiClient} api
 * @returns {import("../domain/ports.js").ServicesGateway}
 */
export function servicesGateway(api) {
  return {
    async getSettings() {
      return toSettings(await api.get("/get_services_settings"));
    },
    async setSettings(runtime) {
      return toSettings(await api.post("/set_services_settings", { container_runtime: runtime }));
    },
  };
}
