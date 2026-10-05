// RailGateway in memory, for tests and web/dev pages; it obeys the same
// contract as the real gateway (../testing/rail-contract.js).

import { FeedStatus } from "../../../shared/domain/feed.js";

/** @param {{ projectId?: string, sessions?: import("../domain/activity.js").RailSession[] }} [world] */
export function memoryRail({ projectId = "p1", sessions = [] } = {}) {
  let list = [...sessions];
  const followers = new Set();

  /** @type {import("../domain/ports.js").RailGateway} */
  const gateway = {
    decodeSeed: (seed) => ({ projectId: seed.project_id, sessions: seed.sessions.map((s) => ({ ...s })) }),
    async listSessions() {
      return [...list];
    },
    follow(pid, onChange, onStatus) {
      const f = { pid, onChange, onStatus };
      followers.add(f);
      queueMicrotask(() => followers.has(f) && onStatus(FeedStatus.LIVE));
      return () => followers.delete(f);
    },
  };

  return {
    gateway,
    /** A session started, changed or went, as the server would announce it. */
    emit(change) {
      list = list.filter((s) => s.id !== (change.kind === "deleted" ? change.id : change.session.id));
      if (change.kind === "upsert") list.push(change.session);
      for (const f of followers) if (f.pid === projectId) f.onChange(change);
    },
    /** Changes the server made while nobody listened: only a list shows them. */
    replace(sessions) {
      list = [...sessions];
    },
    status(s) {
      for (const f of followers) f.onStatus(s);
    },
  };
}
