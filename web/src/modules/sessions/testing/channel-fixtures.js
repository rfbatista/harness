// Task messages and status checks for tests and web/dev pages.

import { T0 } from "./fixtures.js";

/** @returns {import("../domain/channel.js").TaskMessage} */
export function makeMessage(overrides = {}) {
  return Object.freeze({
    id: "m1",
    taskId: "t1",
    fromSessionId: "d1",
    toSessionId: "arch",
    kind: "status_report",
    subject: "",
    body: "Port and scheduler done; tests green.",
    status: "working",
    verdict: "",
    inReplyTo: "",
    documentIds: [],
    artifactIds: [],
    delivered: true,
    deliveredAt: T0,
    createdAt: T0,
    ...overrides,
  });
}

/** The wire format of a task message. */
export function messageDTO(m) {
  return {
    id: m.id,
    task_id: m.taskId,
    from_session_id: m.fromSessionId,
    to_session_id: m.toSessionId,
    kind: m.kind,
    subject: m.subject || undefined,
    body: m.body,
    status: m.status || undefined,
    verdict: m.verdict || undefined,
    in_reply_to: m.inReplyTo || undefined,
    document_ids: m.documentIds,
    artifact_ids: m.artifactIds,
    delivered: m.delivered,
    delivered_at: m.deliveredAt?.toISOString(),
    created_at: m.createdAt.toISOString(),
  };
}

/** @returns {import("../domain/channel.js").StatusCheck} */
export function makeCheck(overrides = {}) {
  return Object.freeze({
    taskId: "t1",
    architectSessionId: "arch",
    delegateSessionId: "d1",
    everyMinutes: 10,
    nextAt: new Date(T0.getTime() + 10 * 60_000),
    lastFiredAt: null,
    firedCount: 0,
    state: "active",
    ...overrides,
  });
}
