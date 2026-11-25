// What the rail feature needs from the outside world.

import type { RailSession, SessionChange } from "../domain/activity";
import type { Task, TaskChange } from "../domain/task";

/** What the server paints the rail from: the project's tasks and sessions, and how many tasks are archived. */
export interface RailSeed {
  readonly projectId: string;
  readonly tasks: readonly Task[];
  readonly sessions: readonly RailSession[];
  readonly archivedCount: number;
}

export type RailSourceErrorCode = "NETWORK_ERROR" | "BAD_RESPONSE" | "UNKNOWN";

export class RailSourceError extends Error {
  readonly code: RailSourceErrorCode;

  constructor(code: RailSourceErrorCode, message: string) {
    super(message);
    this.name = "RailSourceError";
    this.code = code;
  }
}

/** Where the rail's lists are read again from (GET /api/sessions, GET /api/list_tickets). Rejects with a RailSourceError. */
export interface RailSource {
  sessions(projectId: string, signal?: AbortSignal): Promise<RailSession[]>;
  tasks(projectId: string, signal?: AbortSignal): Promise<Task[]>;
  /** The project's archived tasks. */
  archived(projectId: string, signal?: AbortSignal): Promise<Task[]>;
}

/** The project feed's changes that move the rail. */
export type RailChange = TaskChange | SessionChange;

export type RailFeedStatus = "connecting" | "live" | "paused" | "resynced";

/** The project feed (`/events?project_id=` on the shared page stream), as far as the rail reads it. */
export interface RailFeed {
  /** Calls `onChange` for every ticket and session change; returns its unsubscribe. A `task_status` and the project feed's other events are not the rail's. */
  follow(projectId: string, handlers: { onChange(change: RailChange): void; onStatus?(status: RailFeedStatus): void }): () => void;
}

/** Which groups the person folded: one preference per browser, kept in the rail_collapsed cookie the server reads for the first paint. */
export interface RailPrefs {
  setCollapsed(keys: readonly string[]): void;
}

/** Which feature rows the person opened, kept in this browser (ADR 0037); folded by default. */
export interface FeaturePrefs {
  isOpen(featureId: string): boolean;
  setOpen(featureId: string, open: boolean): void;
}

/** Says something to a screen reader through the stream bar's polite region. */
export interface Announcer {
  announce(text: string): void;
}

/** Takes the person to another page (a full page load, or an in-place navigation). */
export interface PageOpener {
  open(url: string): void;
}

/** setTimeout and clearTimeout, so a test moves time by hand. */
export interface Scheduler {
  after(ms: number, run: () => void): () => void;
}
