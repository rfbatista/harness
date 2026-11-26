// The services feed's events and list, as far as the Services item's badge reads them
// (contract: Harness server ↔ Web UI — services): { seq, kind, running_count? } and
// GET /api/list_services: { seq, services: [{ status }] }.

import { array, object, optionalCount, string, type Wire } from "../../../../shared/infrastructure/dto/fields";
import { DtoError } from "../../../../shared/infrastructure/dto/dto-error";

const EVENT = "ServiceEventDto";
const LIST = "ServiceListDto";

export class ServiceEventDto {
  constructor(
    readonly seq: number,
    readonly kind: string,
    /** Absent on an event that carries no count (a service request). */
    readonly running_count: number | undefined,
  ) {}

  static fromJSON(raw: unknown): ServiceEventDto {
    const from = object(EVENT, raw);
    const seq = from["seq"];
    if (typeof seq !== "number" || !Number.isInteger(seq)) throw new DtoError(EVENT, "seq", "expected an integer");
    const kind = typeof from["kind"] === "string" ? from["kind"] : "";
    return new ServiceEventDto(seq, kind, kind === "service_request" ? undefined : (optionalCount(EVENT, from, "running_count") ?? 0));
  }

  toJSON(): Wire {
    return { seq: this.seq, kind: this.kind, ...(this.running_count === undefined ? {} : { running_count: this.running_count }) };
  }
}

export class ServiceListDto {
  constructor(
    readonly seq: number,
    readonly statuses: readonly string[],
  ) {}

  static fromJSON(raw: unknown): ServiceListDto {
    const from = object(LIST, raw);
    const seq = from["seq"];
    return new ServiceListDto(
      typeof seq === "number" && Number.isInteger(seq) ? seq : 0,
      array(LIST, from, "services", (item, at) => string(LIST, object(LIST, item, at), "status", at)),
    );
  }

  toJSON(): Wire {
    return { seq: this.seq, services: this.statuses.map((status) => ({ status })) };
  }
}
