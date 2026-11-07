// ServicesGateway over the harness HTTP API.

import { Codes, StructuredError } from "../../../shared/domain/errors.js";
import { toEvent, toSeed, toServiceList } from "./dto.js";

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
 * @param {import("../../../shared/infrastructure/feed.js").Feed} feed
 * @returns {import("../domain/ports.js").ServicesGateway}
 */
export function servicesGateway(api, feed) {
  return {
    decodeSeed: toSeed,
    async listServices(projectId) {
      return toServiceList(await api.get("/list_services", { project_id: projectId }));
    },
    follow(projectId, onEvent, onStatus) {
      return feed.follow(`/services/events?project_id=${encodeURIComponent(projectId)}`, {
        onMessage: (dto) => {
          let event;
          try {
            event = toEvent(dto);
          } catch {
            return; // an event we cannot read is dropped; the next gap or resync re-reads the list
          }
          onEvent(event);
        },
        onStatus,
      });
    },
    async getSettings() {
      return toSettings(await api.get("/get_services_settings"));
    },
    async setSettings(runtime) {
      return toSettings(await api.post("/set_services_settings", { container_runtime: runtime }));
    },
  };
}
