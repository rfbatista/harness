// The page's one model of the inbox: the global unread count and how the live
// stream is doing. The bell binds to it.

import type { FeedStatus } from "../domain/feed-status";
import type { Change, Notification, NotificationPage } from "../domain/notification";
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

/**
 * A view of some notifications (the panel) that the inbox keeps up to date.
 */
export interface InboxView {
  /** Notifications created, read or resolved. */
  apply(changed: readonly Notification[]): void;
  /** What it shows may be stale: read it again. */
  reload(): void;
}

export class Inbox {
  readonly #source: NotificationsSource;
  readonly #listeners = new Set<(state: InboxState) => void>();
  #state: InboxState = { unreadCount: null, loadFailed: false, feed: "connecting" };
  #started = false;
  readonly #views = new Set<InboxView>();
  #unfollow: (() => void) | null = null;

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

  /** Keeps `view` up to date with what the stream carries; returns its unwatch. */
  watch(view: InboxView): () => void {
    this.#views.add(view);
    return () => void this.#views.delete(view);
  }

  /** Starts once per page, from the server's first paint (`seed`), or from a read when it had none, and follows the stream. */
  start(seed: NotificationPage | null): void {
    if (this.#started) return;
    this.#started = true;
    if (seed) {
      this.#set({ unreadCount: seed.unreadCount });
    } else {
      this.#source.list({ limit: PANEL_LIMIT }).then(
        (page) => this.#heardCount(page.unreadCount),
        () => this.#set({ loadFailed: true }), // the count stays unknown; the stream fills it in
      );
    }
    this.#unfollow = this.#source.follow(
      (change) => this.#heard(change),
      (status) => this.#statusChanged(status),
    );
  }

  /** Stops following the stream. */
  stop(): void {
    this.#unfollow?.();
    this.#unfollow = null;
  }

  /** The stream's count wins over a read that was asked for before it. */
  #heardCount(count: number): void {
    if (this.#heardSomething) return;
    this.#set({ unreadCount: count, loadFailed: false });
  }
  #heardSomething = false;

  #heard(change: Change): void {
    this.#heardSomething = true;
    this.#set({ unreadCount: change.unreadCount, loadFailed: false });
    if (change.notifications.length > 0) for (const view of [...this.#views]) view.apply(change.notifications);
    // An empty change: a deleted project took its notifications along.
    else for (const view of [...this.#views]) view.reload();
  }

  #statusChanged(feed: FeedStatus): void {
    this.#set({ feed });
    if (feed !== "resynced") return;
    // Changes were missed while the stream was down.
    if (this.#views.size > 0) for (const view of [...this.#views]) view.reload();
    else this.#source.list({ limit: 1 }).then((page) => this.#heardCount(page.unreadCount), () => {});
  }

  #set(change: Partial<InboxState>): void {
    this.#state = { ...this.#state, ...change };
    for (const listener of [...this.#listeners]) listener(this.#state);
  }
}
