// ServicesCount over the project's services feed (/services/events) of the shared page stream and GET /api/list_services.

import { ApiError, type ApiClient } from "../../../shared/infrastructure/api-client";
import { DtoError } from "../../../shared/infrastructure/dto/dto-error";
import type { Feed } from "../../../shared/kernel/feed";
import { RailSourceError, type ServicesCount } from "../application/ports";
import { ServiceEventDto, ServiceListDto } from "./dto/services-dto";

/** A service that counts: starting or running. */
const ACTIVE = new Set(["starting", "running"]);

export const servicesFeedPath = (projectId: string): string => `/services/events?project_id=${encodeURIComponent(projectId)}`;

export function httpServicesCount(api: ApiClient, feed: Feed): ServicesCount {
  return {
    follow: (projectId, { onCount, onStatus }) =>
      feed.follow(servicesFeedPath(projectId), {
        onMessage(data) {
          try {
            const event = ServiceEventDto.fromJSON(data);
            if (event.running_count !== undefined) onCount({ seq: event.seq, running: event.running_count });
          } catch (err) {
            // An event that cannot be read is dropped; the next resync reads the list again.
            if (!(err instanceof DtoError)) throw err;
          }
        },
        ...(onStatus ? { onStatus } : {}),
      }),
    async read(projectId, signal) {
      try {
        const list = ServiceListDto.fromJSON(await api.get("/list_services", { project_id: projectId }, signal));
        return { seq: list.seq, running: list.statuses.filter((s) => ACTIVE.has(s)).length };
      } catch (err) {
        if (err instanceof DtoError) throw new RailSourceError("BAD_RESPONSE", err.message);
        if (err instanceof ApiError) throw new RailSourceError(err.code === "NETWORK_ERROR" || err.code === "BAD_RESPONSE" ? err.code : "UNKNOWN", err.message);
        throw err;
      }
    },
  };
}
