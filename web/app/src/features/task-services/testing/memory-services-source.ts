// A ServicesSource in memory, run by hand: the test emits feed events and decides when an action answers.

import type { FeedStatus } from "../../../shared/kernel/feed";
import { ServicesSourceError, type ServiceList, type ServicesEvent, type ServicesFollower, type ServicesSource, type ServiceVerb } from "../application/ports";
import type { LogEvent, LogRead } from "../domain/log";
import type { Decision, ServiceRequest } from "../domain/request";
import type { Service } from "../domain/service";

export interface MemoryServicesSource extends ServicesSource {
  services: Service[];
  pending: ServiceRequest[];
  seq: number;
  listCalls: number;
  acts: { verb: ServiceVerb; serviceId: string; taskId: string }[];
  answers: { requestId: string; decision: Decision }[];
  logText: string;
  logReads: number;
  /** Resolves or rejects the oldest action still waiting. */
  finishAct(result: Service | Error): void;
  failNextAnswer: Error | null;
  emit(event: ServicesEvent): void;
  status(status: FeedStatus): void;
  emitLog(event: LogEvent): void;
  logFollowedFrom: number[];
}

export function memoryServicesSource(init: { services?: Service[]; requests?: ServiceRequest[]; seq?: number } = {}): MemoryServicesSource {
  const followers = new Set<ServicesFollower>();
  const logFollowers = new Set<(e: LogEvent) => void>();
  const waiting: { resolve(s: Service): void; reject(e: Error): void }[] = [];
  const source: MemoryServicesSource = {
    services: init.services ?? [],
    pending: init.requests ?? [],
    seq: init.seq ?? 1,
    listCalls: 0,
    acts: [],
    answers: [],
    logText: "",
    logReads: 0,
    failNextAnswer: null,
    logFollowedFrom: [],
    async list(): Promise<ServiceList> {
      source.listCalls++;
      return { services: source.services, seq: source.seq };
    },
    async requests() {
      return source.pending;
    },
    act(verb, serviceId, taskId) {
      source.acts.push({ verb, serviceId, taskId });
      return new Promise<Service>((resolve, reject) => waiting.push({ resolve, reject }));
    },
    async answer(requestId, decision) {
      source.answers.push({ requestId, decision });
      if (source.failNextAnswer) {
        const err = source.failNextAnswer;
        source.failNextAnswer = null;
        throw err;
      }
      return source.pending.find((r) => r.id === requestId)!;
    },
    follow(_project, follower) {
      followers.add(follower);
      return () => void followers.delete(follower);
    },
    async readLog(): Promise<LogRead> {
      source.logReads++;
      return { text: source.logText, offset: 0, nextOffset: new TextEncoder().encode(source.logText).length, runId: "run-1" };
    },
    followLog(_id, from, onEvent) {
      source.logFollowedFrom.push(from);
      logFollowers.add(onEvent);
      return () => void logFollowers.delete(onEvent);
    },
    finishAct(result) {
      const w = waiting.shift();
      if (!w) throw new Error("no action waiting");
      if (result instanceof Error) w.reject(result);
      else w.resolve(result);
    },
    emit: (event) => followers.forEach((f) => f.onEvent(event)),
    status: (s) => followers.forEach((f) => f.onStatus(s)),
    emitLog: (e) => logFollowers.forEach((f) => f(e)),
  };
  return source;
}

export const refusal = (code: string, message = "refused") => new ServicesSourceError(code, message);
