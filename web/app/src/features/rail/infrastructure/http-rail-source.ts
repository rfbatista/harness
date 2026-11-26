// RailSource over GET /api/sessions and GET /api/list_tickets.

import { ApiError, type ApiClient } from "../../../shared/infrastructure/api-client";
import { DtoError } from "../../../shared/infrastructure/dto/dto-error";
import { RailSourceError, type RailSource } from "../application/ports";
import { SessionListDto, TaskListDto } from "./dto/rail-seed-dto";
import { toSession, toTask } from "./rail-mapper";

async function guarded<T>(read: () => Promise<T>): Promise<T> {
  try {
    return await read();
  } catch (err) {
    if (err instanceof DtoError) throw new RailSourceError("BAD_RESPONSE", err.message);
    if (err instanceof ApiError) throw new RailSourceError(err.code === "NETWORK_ERROR" || err.code === "BAD_RESPONSE" ? err.code : "UNKNOWN", err.message);
    throw err;
  }
}

export function httpRailSource(api: ApiClient): RailSource {
  return {
    sessions: (projectId, signal) => guarded(async () => SessionListDto.fromJSON(await api.get("/sessions", { project_id: projectId }, signal)).sessions.map(toSession)),
    tasks: (projectId, signal) => guarded(async () => TaskListDto.fromJSON(await api.get("/list_tickets", { project_id: projectId }, signal)).tickets.map(toTask)),
    archived: (projectId, signal) => guarded(async () => TaskListDto.fromJSON(await api.get("/list_tickets", { project_id: projectId, archived: "true" }, signal)).tickets.map(toTask)),
  };
}
