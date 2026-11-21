// The page's one model of the inbox: the global unread count and how the live
// stream is doing. The bell binds to it.

import type { FeedStatus } from "../domain/feed-status";
import type { NotificationPage } from "../domain/notification";
import type { NotificationsSource } from "./ports";

/** The latest notifications the top bar's panel shows. */
export const PANEL_LIMIT = 20;

export interface InboxState {
  /** The server's count as last heard; null until heard. */
  readonly unreadCount: number | null;
  /** The first read of the count failed, so it is not known. */
  readonly loadFailed: boolean;
  readonly feed: FeedStatus;
}

export class Inbox {
  readonly #source: NotificationsSource;
  readonly #listeners = new Set<(state: InboxState) => void>();
  #state: InboxState = { unreadCount: null, loadFailed: false, feed: "connecting" };
  #started = false;

  constructor(source: NotificationsSource) {
    this.#source = source;
  }

  get state(): InboxState {
    return this.#state;
  }

  /** Calls `listener` with the state now and with every change; returns its unsubscribe. */
  subscribe(listener: (state: InboxState) => void): () => void {
    this.#listeners.add(listener);
    listener(this.#state);
    return () => void this.#listeners.delete(listener);
  }

  /** Starts once per page, from the server's first paint (`seed`), or from a read when it had none. */
  start(seed: NotificationPage | null): void {
    if (this.#started) return;
    this.#started = true;
    if (seed) {
      this.#set({ unreadCount: seed.unreadCount });
      return;
    }
    this.#source.list({ limit: PANEL_LIMIT }).then(
      (page) => this.#set({ unreadCount: page.unreadCount, loadFailed: false }),
      () => this.#set({ loadFailed: true }), // the count stays unknown
    );
  }

  #set(change: Partial<InboxState>): void {
    this.#state = { ...this.#state, ...change };
    for (const listener of [...this.#listeners]) listener(this.#state);
  }
}
