// ServicesSource over the harness HTTP API and the page event stream.

import { ApiError, type ApiClient } from "../../../shared/infrastructure/api-client";
import { DtoError } from "../../../shared/infrastructure/dto/dto-error";
import type { Feed } from "../../../shared/kernel/feed";
import { ServicesSourceError, type ServicesSource } from "../application/ports";
import { AnswerDto, LogEventDto, LogReadDto, RequestAnswerDto, ServiceActionDto, ServiceAnswerDto, ServiceListDto, ServiceRequestListDto, ServicesEventDto } from "./dto/service-dto";
import { toEvent, toLogEvent, toLogRead, toRequest, toService } from "./service-mapper";

async function mapErrors<T>(call: () => Promise<T>): Promise<T> {
  try {
    return await call();
  } catch (err) {
    if (err instanceof DtoError) throw new ServicesSourceError("BAD_RESPONSE", err.message);
    if (err instanceof ApiError) throw new ServicesSourceError(err.code, err.message);
    throw err;
  }
}

export function httpServicesSource(api: ApiClient, feed: Feed): ServicesSource {
  return {
    list: (projectId, taskId, signal) =>
      mapErrors(async () => {
        const list = ServiceListDto.fromJSON(await api.get("/list_services", { project_id: projectId, task_id: taskId }, signal));
        return { services: list.services.map(toService), seq: list.seq };
      }),
    requests: (projectId, taskId, signal) =>
      mapErrors(async () => ServiceRequestListDto.fromJSON(await api.get("/service_requests", { project_id: projectId, task_id: taskId, state: "pending" }, signal)).requests.map(toRequest)),
    act: (verb, serviceId, taskId) => mapErrors(async () => toService(ServiceAnswerDto.fromJSON(await api.post(`/${verb}_service`, new ServiceActionDto(serviceId, taskId))).service)),
    answer: (requestId, decision) => mapErrors(async () => toRequest(RequestAnswerDto.fromJSON(await api.post(`/service_requests/${encodeURIComponent(requestId)}/answer`, new AnswerDto(decision))).request)),
    follow: (projectId, follower) =>
      feed.follow(`/services/events?project_id=${encodeURIComponent(projectId)}`, {
        onMessage(data) {
          try {
            follower.onEvent(toEvent(ServicesEventDto.fromJSON(data)));
          } catch (err) {
            // An event that does not read is dropped; the next gap or resync re-lists. Anything else is a bug.
            if (!(err instanceof DtoError)) throw err;
          }
        },
        onStatus: (status) => follower.onStatus(status),
      }),
    readLog: (serviceId) => mapErrors(async () => toLogRead(LogReadDto.fromJSON(await api.get("/read_service_log", { service_id: serviceId })))),
    followLog: (serviceId, from, onEvent, onStatus) =>
      feed.follow(`/services/log?service_id=${encodeURIComponent(serviceId)}&from=${from}`, {
        onMessage(data) {
          try {
            onEvent(toLogEvent(LogEventDto.fromJSON(data)));
          } catch (err) {
            if (!(err instanceof DtoError)) throw err;
          }
        },
        onStatus,
      }),
  };
}
