// ServicesGateway over the harness HTTP API.

import { Codes, StructuredError } from "../../../shared/domain/errors.js";
import { toBranchList, toEvent, toLogEvent, toLogRead, toRunVariables, toRunWorktrees, toSeed, toService, toServiceList, toServiceRequest, toServiceRequests } from "./dto.js";

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
    async createService(projectId, input) {
      return toService((await api.post("/create_service", { project_id: projectId, service: input }))?.service);
    },
    async updateService(serviceId, input) {
      const body = await api.post("/update_service", { service_id: serviceId, service: input });
      return { service: toService(body?.service), restartNeeded: body?.restart_needed === true };
    },
    async deleteService(serviceId) {
      await api.post("/delete_service", { service_id: serviceId });
    },
    async startService(serviceId, worktree = "") {
      const body = await api.post("/start_service", worktree === "" ? { service_id: serviceId } : { service_id: serviceId, worktree });
      return { service: toService(body?.service), alreadyRunning: body?.already_running === true };
    },
    async stopService(serviceId) {
      return toService((await api.post("/stop_service", { service_id: serviceId }))?.service);
    },
    async restartService(serviceId) {
      return toService((await api.post("/restart_service", { service_id: serviceId }))?.service);
    },
    async listRunWorktrees(serviceId) {
      return toRunWorktrees(await api.get("/list_service_run_worktrees", { service_id: serviceId }));
    },
    async listRequests(projectId, { state, serviceId } = {}) {
      const query = { project_id: projectId };
      if (state) query.state = state;
      if (serviceId) query.service_id = serviceId;
      return toServiceRequests(await api.get("/service_requests", query));
    },
    async answerRequest(requestId, decision, note = "") {
      const body = await api.post(`/service_requests/${encodeURIComponent(requestId)}/answer`, note === "" ? { decision } : { decision, note });
      return toServiceRequest(body?.request);
    },
    async listBranches(serviceId, query = "") {
      return toBranchList(await api.get("/list_service_branches", query === "" ? { service_id: serviceId } : { service_id: serviceId, query }));
    },
    async getRunVariables(serviceId) {
      return toRunVariables(await api.get("/get_run_variables", { service_id: serviceId }));
    },
    async readLog(serviceId, offset) {
      return toLogRead(await api.get("/read_service_log", { service_id: serviceId, offset: offset === undefined ? undefined : String(offset) }));
    },
    followLog(serviceId, from, onEvent, onStatus) {
      return feed.follow(`/services/log?service_id=${encodeURIComponent(serviceId)}&from=${from}`, {
        onMessage: (dto) => {
          let event;
          try {
            event = toLogEvent(dto);
          } catch {
            return; // an event we cannot read is dropped; the next gap re-reads the log
          }
          onEvent(event);
        },
        onStatus,
      });
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
