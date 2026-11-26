import type { LogEvent, LogRead } from "../domain/log";
import type { ServiceRequest } from "../domain/request";
import type { Service } from "../domain/service";
import type { ServicesEvent } from "../application/ports";
import type { LogEventDto, LogReadDto, ServiceDto, ServiceRequestDto, ServicesEventDto } from "./dto/service-dto";

export function toService(dto: ServiceDto): Service {
  return {
    id: dto.id,
    name: dto.name,
    status: dto.status,
    command: dto.command && { line: dto.command.line, ports: dto.command.ports },
    image: dto.image && { ref: dto.image.ref, ports: dto.image.ports },
    run: dto.run && {
      startedAt: dto.run.started_at,
      endedAt: dto.run.ended_at,
      exitCode: dto.run.exit_code,
      error: dto.run.error,
      startedBy: { kind: dto.run.started_by.kind, taskId: dto.run.started_by.task_id, agent: dto.run.started_by.agent },
    },
    createdBy: { kind: dto.created_by.kind, taskId: dto.created_by.task_id, agent: dto.created_by.agent },
    credentials: dto.credentials,
  };
}

export const toRequest = (dto: ServiceRequestDto): ServiceRequest => ({
  id: dto.id,
  serviceId: dto.service_id,
  serviceName: dto.service_name,
  action: dto.action,
  taskId: dto.task_id,
  agent: dto.agent,
  reason: dto.reason,
  summary: dto.summary,
  state: dto.state,
  requestedAt: dto.requested_at,
});

export function toEvent(dto: ServicesEventDto): ServicesEvent {
  if (dto.kind === "service_changed" && dto.service) return { kind: "service_changed", seq: dto.seq, service: toService(dto.service) };
  if (dto.kind === "service_deleted") return { kind: "service_deleted", seq: dto.seq, serviceId: dto.service_id };
  if (dto.kind === "service_request" && dto.request) return { kind: "service_request", seq: dto.seq, request: toRequest(dto.request) };
  return { kind: "other", seq: dto.seq };
}

export const toLogRead = (dto: LogReadDto): LogRead => ({ text: dto.text, offset: dto.offset, nextOffset: dto.next_offset, runId: dto.run_id });

export const toLogEvent = (dto: LogEventDto): LogEvent => (dto.kind === "reset" ? { kind: "reset", runId: dto.run_id } : { kind: "chunk", runId: dto.run_id, offset: dto.offset, text: dto.text });
