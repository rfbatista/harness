// The API's service, request and log JSON (contract: harness server ↔ task
// services panel). A service that does not read is a DtoError; the optional
// parts of one are lenient.

import { array, lenientArray, object, oneOf, optionalString, string, type Wire } from "../../../../shared/infrastructure/dto/fields";
import { DtoError } from "../../../../shared/infrastructure/dto/dto-error";

const STATUSES = ["stopped", "starting", "running", "exited", "failed"] as const;
const ACTIONS = ["create", "update", "start", "restart", "stop"] as const;
const REQUEST_STATES = ["pending", "approved", "denied", "expired", "done", "failed"] as const;

const NAME = "ServiceDto";

export interface ActorWire {
  kind: "person" | "session";
  task_id: string;
  agent: string;
}

function actor(raw: unknown): ActorWire {
  const from = raw === undefined || raw === null ? {} : object(NAME, raw);
  return { kind: from.kind === "session" ? "session" : "person", task_id: optionalString(NAME, from, "task_id") ?? "", agent: optionalString(NAME, from, "agent") ?? "" };
}

const num = (v: unknown): number => (typeof v === "number" && Number.isFinite(v) ? v : 0);

export class ServiceDto {
  constructor(
    readonly id: string,
    readonly name: string,
    readonly status: (typeof STATUSES)[number],
    readonly command: { line: string; ports: number[] } | null,
    readonly image: { ref: string; ports: { host: number; container: number; protocol: "tcp" | "udp" }[] } | null,
    readonly run: { started_at: string; ended_at: string; exit_code: number | null; error: string; started_by: ActorWire } | null,
    readonly created_by: ActorWire,
    readonly credentials: { label: string; value: string }[],
  ) {}

  static fromJSON(raw: unknown): ServiceDto {
    const from = object(NAME, raw);
    const cmd = from.command === undefined || from.command === null ? null : object(NAME, from.command, "command");
    const img = from.image === undefined || from.image === null ? null : object(NAME, from.image, "image");
    const run = from.run === undefined || from.run === null ? null : object(NAME, from.run, "run");
    return new ServiceDto(
      string(NAME, from, "id"),
      string(NAME, from, "name"),
      oneOf(NAME, from, "status", STATUSES),
      cmd && { line: optionalString(NAME, cmd, "line") ?? "", ports: Array.isArray(cmd.ports) ? cmd.ports.map(num) : [] },
      img && {
        ref: optionalString(NAME, img, "ref") ?? "",
        ports: Array.isArray(img.ports) ? img.ports.map((p) => ({ host: num((p as Wire)?.host), container: num((p as Wire)?.container), protocol: (p as Wire)?.protocol === "udp" ? "udp" : "tcp" })) : [],
      },
      run && {
        started_at: optionalString(NAME, run, "started_at") ?? "",
        ended_at: optionalString(NAME, run, "ended_at") ?? "",
        exit_code: Number.isInteger(run.exit_code) ? (run.exit_code as number) : null,
        error: optionalString(NAME, run, "error") ?? "",
        started_by: actor(run.started_by),
      },
      actor(from.created_by),
      lenientArray(NAME, from, "credentials", (item, at) => {
        const c = object(NAME, item, at);
        return { label: string(NAME, c, "label", at), value: string(NAME, c, "value", at) };
      }),
    );
  }
}

export class ServiceListDto {
  constructor(
    readonly services: ServiceDto[],
    readonly seq: number,
  ) {}

  static fromJSON(raw: unknown): ServiceListDto {
    const from = object("ServiceListDto", raw);
    return new ServiceListDto(array("ServiceListDto", from, "services", (item) => ServiceDto.fromJSON(item)), num(from.seq));
  }
}

/** The answer of start, stop and restart: `{ service }`. */
export class ServiceAnswerDto {
  constructor(readonly service: ServiceDto) {}
  static fromJSON(raw: unknown): ServiceAnswerDto {
    return new ServiceAnswerDto(ServiceDto.fromJSON(object("ServiceAnswerDto", raw).service));
  }
}

const REQ = "ServiceRequestDto";

export class ServiceRequestDto {
  constructor(
    readonly id: string,
    readonly service_id: string,
    readonly service_name: string,
    readonly action: (typeof ACTIONS)[number],
    readonly task_id: string,
    readonly agent: string,
    readonly reason: string,
    readonly summary: string,
    readonly state: (typeof REQUEST_STATES)[number],
    readonly requested_at: string,
  ) {}

  /** Credential values a request may carry are never read: the panel shows none. */
  static fromJSON(raw: unknown): ServiceRequestDto {
    const from = object(REQ, raw);
    const id = string(REQ, from, "id");
    if (id === "") throw new DtoError(REQ, "id", "expected an id");
    return new ServiceRequestDto(
      id,
      optionalString(REQ, from, "service_id") ?? "",
      optionalString(REQ, from, "service_name") ?? "",
      oneOf(REQ, from, "action", ACTIONS),
      optionalString(REQ, from, "task_id") ?? "",
      optionalString(REQ, from, "agent") ?? "",
      optionalString(REQ, from, "reason") ?? "",
      optionalString(REQ, from, "summary") ?? "",
      oneOf(REQ, from, "state", REQUEST_STATES),
      optionalString(REQ, from, "requested_at") ?? "",
    );
  }
}

export class ServiceRequestListDto {
  constructor(readonly requests: ServiceRequestDto[]) {}
  static fromJSON(raw: unknown): ServiceRequestListDto {
    return new ServiceRequestListDto(array("ServiceRequestListDto", object("ServiceRequestListDto", raw), "requests", (item) => ServiceRequestDto.fromJSON(item)));
  }
}

export class RequestAnswerDto {
  constructor(readonly request: ServiceRequestDto) {}
  static fromJSON(raw: unknown): RequestAnswerDto {
    return new RequestAnswerDto(ServiceRequestDto.fromJSON(object("RequestAnswerDto", raw).request));
  }
}

const EVT = "ServicesEventDto";

export class ServicesEventDto {
  constructor(
    readonly seq: number,
    readonly kind: string,
    readonly service: ServiceDto | undefined,
    readonly request: ServiceRequestDto | undefined,
    readonly service_id: string,
  ) {}

  static fromJSON(raw: unknown): ServicesEventDto {
    const from = object(EVT, raw);
    if (!Number.isInteger(from.seq)) throw new DtoError(EVT, "seq", "expected an event with a seq");
    const kind = string(EVT, from, "kind");
    return new ServicesEventDto(
      from.seq as number,
      kind,
      kind === "service_changed" ? ServiceDto.fromJSON(from.service) : undefined,
      kind === "service_request" ? ServiceRequestDto.fromJSON(from.request) : undefined,
      optionalString(EVT, from, "service_id") ?? "",
    );
  }
}

export class LogReadDto {
  constructor(
    readonly text: string,
    readonly offset: number,
    readonly next_offset: number,
    readonly run_id: string,
  ) {}
  static fromJSON(raw: unknown): LogReadDto {
    const from = object("LogReadDto", raw);
    return new LogReadDto(string("LogReadDto", from, "text"), num(from.offset), num(from.next_offset), optionalString("LogReadDto", from, "run_id") ?? "");
  }
}

export class LogEventDto {
  constructor(
    readonly kind: "chunk" | "reset",
    readonly run_id: string,
    readonly offset: number,
    readonly text: string,
  ) {}
  static fromJSON(raw: unknown): LogEventDto {
    const from = object("LogEventDto", raw);
    return new LogEventDto(oneOf("LogEventDto", from, "kind", ["chunk", "reset"] as const), optionalString("LogEventDto", from, "run_id") ?? "", num(from.offset), optionalString("LogEventDto", from, "text") ?? "");
  }
}

/** The body of start, stop and restart from the panel: the service and the task the run is recorded on. */
export class ServiceActionDto {
  constructor(
    readonly service_id: string,
    readonly task_id: string,
  ) {}
  toJSON(): Wire {
    return { service_id: this.service_id, task_id: this.task_id };
  }
}

export class AnswerDto {
  constructor(readonly decision: "approve" | "deny") {}
  toJSON(): Wire {
    return { decision: this.decision };
  }
}
