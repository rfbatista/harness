// A pedido de serviço: an agent session's request to create, change, start,
// stop or restart a service, waiting for the person.

export type RequestAction = "create" | "update" | "start" | "restart" | "stop";
export type RequestState = "pending" | "approved" | "denied" | "expired" | "done" | "failed";

export interface ServiceRequest {
  readonly id: string;
  readonly serviceId: string;
  readonly serviceName: string;
  readonly action: RequestAction;
  readonly taskId: string;
  readonly agent: string;
  readonly reason: string;
  readonly summary: string;
  readonly state: RequestState;
  readonly requestedAt: string;
}

export type Decision = "approve" | "deny";
