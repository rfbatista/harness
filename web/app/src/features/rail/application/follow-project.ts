// The rail's model of a project: the server's seed on first paint, then the
// project feed's changes (ADR 0002). It never reads a client store shared with
// another UI; it holds the tasks, the sessions and the archived count, and
// tells whoever listens what each change did.

import { activityByTask, applySessionChange, type Activity, type RailSession } from "../domain/activity";
import { applyTaskChange, onBoard, type Task } from "../domain/task";
import type { RailChange, RailFeed, RailFeedStatus, RailSeed, RailSource } from "./ports";

export interface RailState {
  readonly tasks: readonly Task[];
  readonly sessions: readonly RailSession[];
  readonly archivedCount: number;
}

/** By task id, what the rail shows of its sessions. */
export const activityOf = (state: RailState): Record<string, Activity> => activityByTask(state.sessions);

/** What changed the model: a feed change, or the lists read again after a drop (not news: no wash, no announcement). */
export interface RailUpdate {
  readonly cause: "change" | "resync";
  readonly before: RailState;
  readonly state: RailState;
  readonly change?: RailChange;
}

export interface ProjectFollow {
  state(): RailState;
  stop(): void;
}

export interface FollowProjectInput {
  readonly source: RailSource;
  readonly feed: RailFeed;
  readonly seed: RailSeed;
  onUpdate(update: RailUpdate): void;
  onStatus?(status: RailFeedStatus): void;
}

export const seedState = (seed: RailSeed): RailState => ({ tasks: onBoard(seed.tasks), sessions: seed.sessions, archivedCount: seed.archivedCount });

/**
 * Follows the project's feed from the seed. On the feed's first live, and on
 * every resync, the lists are read again: the seed was painted before the
 * stream opened, and what the server announced in between never reached the
 * page. Sessions and tasks are refreshed independently, so one list failing
 * keeps the other current.
 */
export function followProject({ source, feed, seed, onUpdate, onStatus }: FollowProjectInput): ProjectFollow {
  const projectId = seed.projectId;
  let state = seedState(seed);
  let wentLive = false;
  let stopped = false;

  const set = (next: RailState, cause: RailUpdate["cause"], change?: RailChange): void => {
    const before = state;
    state = next;
    if (!stopped) onUpdate({ cause, before, state, ...(change ? { change } : {}) });
  };

  async function recountArchived(): Promise<void> {
    try {
      const archived = (await source.archived(projectId)).length;
      set({ ...state, archivedCount: archived }, "resync");
    } catch {
      // keeps the count it shows until the next change or resync
    }
  }

  async function reloadTasks(): Promise<void> {
    try {
      const tasks = onBoard(await source.tasks(projectId));
      set({ ...state, tasks }, "resync");
    } catch {
      // keeps what it shows until the next change or resync
    }
  }

  async function resync(): Promise<void> {
    const [sessions, tasks] = await Promise.allSettled([source.sessions(projectId), source.tasks(projectId)]);
    let next = state;
    if (sessions.status === "fulfilled") next = { ...next, sessions: sessions.value };
    if (tasks.status === "fulfilled") next = { ...next, tasks: onBoard(tasks.value) };
    set(next, "resync");
    await recountArchived();
  }

  function apply(change: RailChange): void {
    if (change.kind === "task-upsert" || change.kind === "task-deleted") {
      const id = change.kind === "task-deleted" ? change.id : change.task.id;
      const previous = state.tasks.find((t) => t.id === id) ?? null;
      let archivedCount = state.archivedCount;
      let readAgain = false;
      if (change.kind === "task-upsert") {
        if (change.task.archivedAt && previous) archivedCount += 1;
        // A done task the store did not have may be a restore (or a new task): only the server knows the count and the children archived on their own.
        else if (!change.task.archivedAt && !previous && change.task.status === "done") readAgain = true;
      }
      set({ ...state, tasks: applyTaskChange(state.tasks, change), archivedCount }, "change", change);
      if (readAgain) void Promise.all([recountArchived(), reloadTasks()]);
    } else {
      set({ ...state, sessions: applySessionChange(state.sessions, change) }, "change", change);
    }
  }

  const unfollow = feed.follow(projectId, {
    onChange: apply,
    onStatus(status) {
      onStatus?.(status);
      const first = status === "live" && !wentLive;
      if (status === "live") wentLive = true;
      if (first || status === "resynced") void resync();
    },
  });

  return {
    state: () => state,
    stop() {
      stopped = true;
      unfollow();
    },
  };
}
