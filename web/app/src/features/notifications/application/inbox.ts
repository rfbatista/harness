// The page's one model of the inbox: the global unread count, which
// notifications it has seen, the reads in flight and how the live stream is
// doing. The bell and the panel bind to it, and it follows the notifications
// stream once per page. Ported from the Alpine client's notifications store.
//
// The count and the echo. Marking read is optimistic: the rows change and the
// count drops at once. The stream echoes the read, possibly before the POST
// answers, and messages committed before the read can still arrive carrying
// the old count and the unread copy. Until the server is seen to have read an
// id (its echo, or the answer), the id stays pending: its unread copies are
// ignored and it stays off the count. When the POST answers, its count wins
// unless the echo came first: the stream is in commit order, so after the
// echo the stream's count is the later one.

import type { FeedStatus } from "../domain/feed-status";
import { isOpen, isUnread, markedRead, type Change, type Notification, type NotificationPage } from "../domain/notification";
import { NotificationsSourceError, type Navigator, type NotificationsSource } from "./ports";

/** The latest notifications the top bar's panel shows. */
export const PANEL_LIMIT = 20;
/** How long opening a notification waits for its read before it navigates. */
export const OPEN_WAIT_MS = 1_500;

export interface InboxState {
  /** The unread count to show; null while unknown. */
  readonly unreadCount: number | null;
  /** What the bell and its name count: the causes that need the person (ADR 0096), as the server says, or as far as this page has seen. */
  readonly badgeCount: number;
  /** The first read of the count failed, so it is not known. */
  readonly loadFailed: boolean;
  readonly feed: FeedStatus;
}

/** A view of some notifications (the panel) that the inbox keeps up to date. */
export interface InboxView {
  /** Notifications created, read or resolved. */
  apply(changed: readonly Notification[]): void;
  /** Every one it shows is read as of `at`. Returns what puts them back as they were, for a read that fails. */
  readAll(at: Date): () => void;
  /** What it shows may be stale: read it again. */
  reload(): void;
}

export interface InboxOptions {
  readonly navigator: Navigator;
  readonly now?: () => Date;
  readonly wait?: (ms: number) => Promise<void>;
}

export class Inbox {
  readonly #source: NotificationsSource;
  readonly #navigator: Navigator;
  readonly #now: () => Date;
  readonly #wait: (ms: number) => Promise<void>;
  readonly #listeners = new Set<(state: InboxState) => void>();
  readonly #views = new Set<InboxView>();
  /** Ids this page has shown or heard of. */
  readonly #seen = new Set<string>();
  /** Ids read here that the server is not yet seen to have read. */
  readonly #pendingRead = new Set<string>();
  /** Whether a Mark all read is in flight, and how many. */
  #pendingAll = 0;
  /** The reads in flight, each with the ids the stream showed read meanwhile. */
  readonly #requests = new Set<{ seenRead: Set<string> }>();
  /** Ids this page has seen that are open causes: what the bell counts when the server sends no open_count. */
  readonly #openIds = new Set<string>();
  #serverCount: number | null = null;
  /** The open causes as last heard; null until heard, and from a server that sends none. */
  #serverOpen: number | null = null;
  #loadFailed = false;
  #feed: FeedStatus = "connecting";
  /** Counts stream messages, so an answer can tell whether one came meanwhile. */
  #stamp = 0;
  #state: InboxState = { unreadCount: null, badgeCount: 0, loadFailed: false, feed: "connecting" };
  #started = false;
  #unfollow: (() => void) | null = null;

  constructor(source: NotificationsSource, { navigator, now = () => new Date(), wait = (ms) => new Promise((resolve) => setTimeout(resolve, ms)) }: InboxOptions) {
    this.#source = source;
    this.#navigator = navigator;
    this.#now = now;
    this.#wait = wait;
  }

  get state(): InboxState {
    return this.#state;
  }

  /** Counts stream messages: pass it to `listed` so a read asked for before a message does not overwrite its count. */
  get stamp(): number {
    return this.#stamp;
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
      for (const n of seed.notifications) this.#seen.add(n.id);
      this.track(seed.notifications);
      this.#heardOpen(seed.openCount);
      this.#serverCount = seed.unreadCount;
      this.#publish();
    } else {
      const since = this.#stamp;
      this.#source.list({ limit: PANEL_LIMIT }).then(
        (page) => this.listed(page, since),
        () => {
          this.#loadFailed = true; // the count stays unknown; the stream fills it in
          this.#publish();
        },
      );
    }
    this.#unfollow = this.#source.follow(
      (change) => this.#heard(change),
      (feed) => this.#statusChanged(feed),
    );
  }

  /** Stops following the stream. */
  stop(): void {
    this.#unfollow?.();
    this.#unfollow = null;
  }

  /**
   * A view read a page. Its count is the server's unless a stream message came
   * while it was asked for (`since` is the stamp when it asked).
   */
  listed(page: NotificationPage, since: number): void {
    for (const n of page.notifications) this.#seen.add(n.id);
    this.track(page.notifications);
    if (this.#stamp !== since) return;
    this.#serverCount = page.unreadCount;
    this.#heardOpen(page.openCount);
    this.#loadFailed = false;
    this.#publish();
  }

  /** Marks these read: at once on screen, then on the server. Rejects with the source's error, after putting the rows back. */
  async markRead(targets: readonly Notification[]): Promise<void> {
    const unread = targets.filter((n) => isUnread(n) && !this.#pendingRead.has(n.id));
    if (unread.length === 0) return;
    const ids = unread.map((n) => n.id);
    const at = this.#now();
    for (const id of ids) this.#pendingRead.add(id);
    this.#publish();
    this.#each((view) => view.apply(unread.map((n) => markedRead(n, at))));

    const request = { seenRead: new Set<string>() };
    this.#requests.add(request);
    try {
      this.#answered(await this.#source.markRead({ ids }), request);
    } catch (err) {
      // Nothing was marked: the rows go back as they were.
      this.#each((view) => view.apply(unread.filter((n) => !request.seenRead.has(n.id))));
      if (err instanceof NotificationsSourceError && err.code === "NOTIFICATION_NOT_FOUND") this.#each((view) => view.reload());
      throw err;
    } finally {
      this.#requests.delete(request);
      for (const id of ids) this.#pendingRead.delete(id);
      this.#publish();
    }
  }

  /** Marks every unread notification read. */
  async markAllRead(): Promise<void> {
    const at = this.#now();
    this.#pendingAll += 1;
    this.#publish();
    const undo = [...this.#views].map((view) => view.readAll(at));

    const request = { seenRead: new Set<string>() };
    this.#requests.add(request);
    try {
      this.#answered(await this.#source.markRead({ all: true }), request);
    } catch (err) {
      // Nothing was marked: the rows go back as they were, and are read again.
      for (const restore of undo) restore();
      this.#each((view) => view.reload());
      throw err;
    } finally {
      this.#requests.delete(request);
      this.#pendingAll -= 1;
      this.#publish();
    }
  }

  /**
   * Opens a notification at its link and marks it read. With `keepPage` (a new
   * tab, a modifier click) the page stays and the notification is only marked.
   */
  async open(n: Notification, { keepPage = false }: { keepPage?: boolean } = {}): Promise<void> {
    const read = this.markRead([n]).catch(() => {});
    if (keepPage) {
      await read;
      return;
    }
    await Promise.race([read, this.#wait(OPEN_WAIT_MS)]);
    this.#navigator.open(n.link);
  }

  /** Notes which of these are open causes. */
  track(notifications: readonly Notification[]): void {
    for (const n of notifications) {
      if (isOpen(n)) this.#openIds.add(n.id);
      else this.#openIds.delete(n.id);
    }
    this.#publish();
  }

  /** Takes the server's open count, when it sent one. */
  #heardOpen(openCount: number | null | undefined): void {
    if (openCount !== null && openCount !== undefined) this.#serverOpen = openCount;
  }

  #each(fn: (view: InboxView) => void): void {
    for (const view of [...this.#views]) fn(view);
  }

  /** Whether a copy from the server is one the person has already read here. */
  #stale(n: Notification): boolean {
    return isUnread(n) && (this.#pendingRead.has(n.id) || (this.#seen.has(n.id) && this.#pendingAll > 0));
  }

  #heard(change: Change): void {
    this.#stamp += 1;
    // Judged before they count as seen: a notification that arrives while a read is in flight is news, not a copy of one read here.
    const changed = change.notifications.filter((n) => !this.#stale(n));
    for (const n of change.notifications) {
      if (!isUnread(n)) {
        this.#pendingRead.delete(n.id);
        for (const request of this.#requests) request.seenRead.add(n.id);
      }
      this.#seen.add(n.id);
    }
    this.track(change.notifications);
    this.#serverCount = change.unreadCount;
    this.#heardOpen(change.openCount);
    this.#loadFailed = false;
    this.#publish();

    if (changed.length > 0) this.#each((view) => view.apply(changed));
    // An empty change: a deleted project took its notifications along.
    if (change.notifications.length === 0) this.#each((view) => view.reload());
  }

  #statusChanged(feed: FeedStatus): void {
    this.#feed = feed;
    this.#publish();
    if (feed !== "resynced") return;
    // Changes were missed while the stream was down: the count is read again, and so is what the views show.
    const since = this.#stamp;
    this.#source.list({ limit: 1 }).then((page) => this.listed({ ...page, notifications: [] }, since), () => {});
    this.#each((view) => view.reload());
  }

  #answered(answer: Change, request: { seenRead: Set<string> }): void {
    for (const n of answer.notifications) this.#seen.add(n.id);
    this.track(answer.notifications);
    const echoed = answer.notifications.some((n) => request.seenRead.has(n.id));
    if (!echoed) {
      this.#serverCount = answer.unreadCount;
      this.#heardOpen(answer.openCount);
    }
    if (answer.notifications.length > 0) this.#each((view) => view.apply(answer.notifications));
  }

  /** The state to show: reads in flight are already off the count. */
  #publish(): void {
    const unreadCount = this.#serverCount === null ? null : this.#pendingAll > 0 ? 0 : Math.max(0, this.#serverCount - this.#pendingRead.size);
    this.#state = { unreadCount, badgeCount: this.#serverOpen ?? this.#openIds.size, loadFailed: this.#loadFailed, feed: this.#feed };
    for (const listener of [...this.#listeners]) listener(this.#state);
  }
}
