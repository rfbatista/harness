// ChannelGateway in memory: the fake for page tests and the data source for
// web/dev pages. It obeys the same contract as the real gateway
// (../testing/channel-contract.js).

import { Codes, StructuredError } from "../../../shared/domain/errors.js";
import { FeedStatus } from "../../../shared/domain/feed.js";
import { upsertMessage } from "../domain/channel.js";

/**
 * @param {{
 *   projectId?: string,
 *   messages?: import("../domain/channel.js").TaskMessage[],
 *   checks?: import("../domain/channel.js").StatusCheck[],
 *   now?: () => Date,
 *   onCheck?: (check: import("../domain/channel.js").StatusCheck) => void,
 * }} [world]
 *   projectId: the project every task here belongs to.
 *   onCheck: told of every changed check, as the server then announces the
 *   delegate's session (web/dev wires it to the sessions gateway).
 */
export function memoryChannel({ projectId = "p1", messages = [], checks = [], now = () => new Date(), onCheck = () => {} } = {}) {
  let list = messages.reduce(upsertMessage, []);
  const loops = new Map(checks.map((c) => [c.delegateSessionId, c]));
  const followers = new Set();
  let nextId = 1;

  const announce = (message) => {
    for (const f of followers) if (f.projectId === projectId) f.onEvent({ kind: "message", message });
  };

  /** @type {import("../domain/ports.js").ChannelGateway} */
  const gateway = {
    async listMessages({ ticketId, sessionId, since }) {
      return list.filter(
        (m) =>
          m.taskId === ticketId &&
          (!sessionId || m.fromSessionId === sessionId || m.toSessionId === sessionId) &&
          (!since || m.createdAt.getTime() > since.getTime()),
      );
    },
    async setStatusCheck(delegateSessionId, everyMinutes) {
      const check = loops.get(delegateSessionId);
      if (!check) throw new StructuredError(Codes.STATUS_CHECK_NOT_FOUND, `session ${delegateSessionId} has no status check`, 404);
      if (!Number.isInteger(everyMinutes) || (everyMinutes !== 0 && (everyMinutes < 2 || everyMinutes > 240))) {
        throw new StructuredError(Codes.INVALID_INPUT, "every_minutes must be 0, or between 2 and 240", 400);
      }
      const changed = Object.freeze({
        ...check,
        everyMinutes,
        state: everyMinutes === 0 ? "paused" : "active",
        nextAt: everyMinutes === 0 ? null : new Date(now().getTime() + everyMinutes * 60_000),
      });
      loops.set(delegateSessionId, changed);
      onCheck(changed);
      return changed;
    },
    follow(id, onEvent, onStatus) {
      const follower = { projectId: id, onEvent };
      followers.add(follower);
      queueMicrotask(() => followers.has(follower) && onStatus(FeedStatus.LIVE));
      return () => followers.delete(follower);
    },
  };

  return {
    gateway,
    /** A session sends a message, as message_architect or reply_to_session would. */
    send(fields) {
      const message = Object.freeze({
        id: `m-${nextId++}`,
        subject: "",
        status: "",
        verdict: "",
        inReplyTo: "",
        documentIds: [],
        artifactIds: [],
        delivered: true,
        deliveredAt: now(),
        createdAt: now(),
        ...fields,
      });
      list = upsertMessage(list, message);
      announce(message);
      return message;
    },
    /** A message stored while its recipient was not running reaches it. */
    deliver(id) {
      const m = list.find((x) => x.id === id);
      if (!m) return null;
      const delivered = Object.freeze({ ...m, delivered: true, deliveredAt: now() });
      list = upsertMessage(list, delivered);
      announce(delivered);
      return delivered;
    },
    /** The loop on a delegate fires, as the server's scheduler would. */
    fire(delegateSessionId) {
      const check = loops.get(delegateSessionId);
      if (!check) return null;
      const fired = Object.freeze({ ...check, lastFiredAt: now(), firedCount: check.firedCount + 1, nextAt: new Date(now().getTime() + check.everyMinutes * 60_000) });
      loops.set(delegateSessionId, fired);
      onCheck(fired);
      return fired;
    },
    check: (delegateSessionId) => loops.get(delegateSessionId) ?? null,
  };
}
