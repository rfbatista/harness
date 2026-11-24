// What the product owner feature needs from the outside world.

import type { FeedStatus } from "../domain/feed-status";
import type { ProductOwnerEvent, Settings, ThreadPage, ThreadSummary } from "../domain/thread";

export type ProductOwnerErrorCode =
  | "PRODUCT_OWNER_OFF"
  | "THREAD_BUSY"
  | "INVALID_INPUT"
  | "PRODUCT_OWNER_UNAVAILABLE"
  | "PROJECT_NOT_FOUND"
  | "THREAD_NOT_FOUND"
  | "NETWORK_ERROR"
  | "BAD_RESPONSE"
  | "UNKNOWN";

export class ProductOwnerError extends Error {
  readonly code: ProductOwnerErrorCode;
  /** PRODUCT_OWNER_UNAVAILABLE: where the person fixes it. */
  readonly fixUrl: string | undefined;

  constructor(code: ProductOwnerErrorCode, message: string, fixUrl?: string) {
    super(message);
    this.name = "ProductOwnerError";
    this.code = code;
    this.fixUrl = fixUrl;
  }
}

/** One project's product owner over the harness API. Every call rejects with a ProductOwnerError. */
export interface ProductOwnerGateway {
  settings(projectId: string, signal?: AbortSignal): Promise<Settings>;
  setEnabled(projectId: string, enabled: boolean): Promise<Settings>;
  /** The most recently active first. */
  threads(projectId: string, signal?: AbortSignal): Promise<readonly ThreadSummary[]>;
  /** A page of the thread, oldest first; `before` pages towards the start. */
  thread(threadId: string, before?: string, signal?: AbortSignal): Promise<ThreadPage>;
  startThread(projectId: string, message: string): Promise<{ threadId: string; itemId: string }>;
  sendMessage(threadId: string, message: string): Promise<{ itemId: string }>;
  stopTurn(threadId: string): Promise<void>;
  /** Answers the renamed thread. */
  renameThread(threadId: string, title: string): Promise<ThreadSummary>;
}

/** The page stream's product-owner events for one project; events of another project never arrive. */
export interface ProductOwnerEvents {
  /** Returns what stops following. `onStatus` says when what is on screen may be stale. */
  follow(projectId: string, onEvent: (event: ProductOwnerEvent) => void, onStatus: (status: FeedStatus) => void): () => void;
}

/** What the panel remembers in the browser: open or closed and the width, for the whole browser; the open thread, per project. */
export interface DockMemory {
  load(projectId: string): RememberedDock;
  save(projectId: string, state: RememberedDock): void;
}

export interface RememberedDock {
  readonly open: boolean;
  /** In rem. */
  readonly width: number;
  readonly threadId: string | null;
}

/** The screen the panel docks on: under 64 rem there is no room for a third column. */
export interface Screen {
  narrow(): boolean;
  /** Returns what stops listening. */
  onChange(listener: () => void): () => void;
}

/** Opens a page of the web UI. */
export interface PageNavigator {
  open(href: string): void;
}

/** What the page knows of a project's tasks, so a task named in an answer shows its status. */
export interface TaskCatalog {
  /** null for a task the page does not know. */
  find(taskId: string): { readonly state: "running" | "waiting" | "done" | "idle" } | null;
}
