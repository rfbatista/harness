// What the task services panel needs from the outside world.

/** The feed as the panel sees it. */
export type LiveStatus = "connecting" | "live" | "paused" | "resynced";
import type { Decision, ServiceRequest } from "../domain/request";
import type { LogEvent, LogRead } from "../domain/log";
import type { Service } from "../domain/service";

export class ServicesSourceError extends Error {
  readonly code: string;
  constructor(code: string, message: string) {
    super(message);
    this.name = "ServicesSourceError";
    this.code = code;
  }
}

export interface ServiceList {
  readonly services: readonly Service[];
  /** The services feed's sequence number the list is current to. */
  readonly seq: number;
}

/** One event of the services feed. `service` and `request` are the ones the kind carries. */
export type ServicesEvent =
  | { readonly kind: "service_changed"; readonly seq: number; readonly service: Service }
  | { readonly kind: "service_deleted"; readonly seq: number; readonly serviceId: string }
  | { readonly kind: "service_request"; readonly seq: number; readonly request: ServiceRequest }
  | { readonly kind: "other"; readonly seq: number };

export interface ServicesFollower {
  onEvent(event: ServicesEvent): void;
  onStatus(status: LiveStatus): void;
}

export type ServiceVerb = "start" | "stop" | "restart";

export interface ServicesSource {
  /** The serviços da task. */
  list(projectId: string, taskId: string, signal?: AbortSignal): Promise<ServiceList>;
  /** The task's pending pedidos de serviço. */
  requests(projectId: string, taskId: string, signal?: AbortSignal): Promise<readonly ServiceRequest[]>;
  /** A person's start, stop or restart, recording the task. The answer is the service as it is; the feed settles it. */
  act(verb: ServiceVerb, serviceId: string, taskId: string): Promise<Service>;
  answer(requestId: string, decision: Decision): Promise<ServiceRequest>;
  follow(projectId: string, follower: ServicesFollower): () => void;
  readLog(serviceId: string): Promise<LogRead>;
  followLog(serviceId: string, from: number, onEvent: (event: LogEvent) => void, onStatus: (status: LiveStatus) => void): () => void;
}

export interface Clipboard {
  copy(text: string): Promise<void>;
}

/** What the person chose for the panel in this browser. */
export interface PanelPreferences {
  /** null when never chosen (or unreadable). */
  open(taskId: string): boolean | null;
  saveOpen(taskId: string, open: boolean): void;
}

export interface Scheduler {
  now(): number;
  /** Runs `fn` after `ms`; returns what cancels it. */
  after(ms: number, fn: () => void): () => void;
}
