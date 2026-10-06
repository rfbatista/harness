// RailGateway in memory, for tests and web/dev pages; it obeys the same
// contract as the real gateway (../testing/rail-contract.js).

import { FeedStatus } from "../../../shared/domain/feed.js";
import { toTask } from "./dto.js";

/**
 * @param {{
 *   projectId?: string,
 *   sessions?: import("../domain/activity.js").RailSession[],
 *   tasks?: import("../domain/task.js").Task[],
 * }} [world]
 */
export function memoryRail({ projectId = "p1", sessions = [], tasks = [] } = {}) {
  let list = [...sessions];
  let taskList = [...tasks];
  const followers = new Set();

  /** @type {import("../domain/ports.js").RailGateway} */
  const gateway = {
    decodeSeed: (seed) => ({ projectId: seed.project_id, sessions: seed.sessions.map((s) => ({ ...s })), tasks: seed.tasks.map(toTask) }),
    async listSessions() {
      return [...list];
    },
    async listTasks() {
      return [...taskList];
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
    /** A session or a task started, changed or went, as the server would announce it. */
    emit(change) {
      if (change.kind === "task-upsert" || change.kind === "task-deleted") {
        const id = change.kind === "task-deleted" ? change.id : change.task.id;
        taskList = taskList.filter((t) => t.id !== id);
        if (change.kind === "task-upsert") taskList.push(change.task);
      } else {
        list = list.filter((s) => s.id !== (change.kind === "deleted" ? change.id : change.session.id));
        if (change.kind === "upsert") list.push(change.session);
      }
      for (const f of followers) if (f.pid === projectId) f.onChange(change);
    },
    /** Changes the server made while nobody listened: only a list shows them. */
    replace(sessions) {
      list = [...sessions];
    },
    /** Tasks changed while nobody listened: only a list shows them. */
    replaceTasks(tasks) {
      taskList = [...tasks];
    },
    status(s) {
      for (const f of followers) f.onStatus(s);
    },
  };
}
