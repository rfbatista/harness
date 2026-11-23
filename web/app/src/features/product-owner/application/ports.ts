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
