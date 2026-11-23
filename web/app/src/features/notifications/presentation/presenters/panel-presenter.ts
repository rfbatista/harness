// The panel behind the bell: the latest notifications across every project,
// All or Unread, opened one at a time, marked read one by one or all at once.
// It is one of the inbox's views: the stream's changes reach it there.

import type { FeedStatus } from "../../domain/feed-status";
import type { Clock } from "../../../../shared/kernel/clock";
import { Presenter } from "../../../../shared/kernel/presenter";
import { Inbox, PANEL_LIMIT } from "../../application/inbox";
import { loadPanel } from "../../application/load-panel";
import type { NotificationsSource } from "../../application/ports";
import { isOpen, markedRead, merge, openFirst, upsert, type Notification, type NotificationPage } from "../../domain/notification";
import { inboxErrorMessage, streamNotice } from "./inbox-messages";
import { toRow, type Row } from "./row";

/** Where the view puts the keyboard focus: the presenter states the intent, the view carries it out. */
export interface FocusRequest {
  readonly target: "bell" | "panel";
  /** A new id is a new request, even for the same target. */
  readonly id: number;
}

export interface PanelViewModel {
  readonly open: boolean;
  readonly unreadOnly: boolean;
  readonly rows: readonly Row[];
  readonly loading: boolean;
  readonly empty: boolean;
  readonly emptyText: string;
  readonly canMarkAll: boolean;
  /** The inbox page: "See all notifications". */
  readonly inboxHref: string;
  /** A failed load or action, in plain words, shown with Reload and Dismiss buttons. */
  readonly error: string | null;
  /** The line a dropped stream shows with a Reload button; "" while the stream is fine. */
  readonly streamNotice: string;
  readonly focus: FocusRequest | null;
}

interface State {
  items: readonly Notification[];
  loaded: boolean;
  /** The open causes older than the latest page have been read (the seed holds only the latest). */
  openLoaded: boolean;
  loading: boolean;
  unreadOnly: boolean;
  open: boolean;
  error: string | null;
  /** Null until the view has mounted: ages are not part of the server render, which could not match them. */
  now: Date | null;
  unreadCount: number | null;
  feed: FeedStatus;
  focus: FocusRequest | null;
}

export class PanelPresenter extends Presenter<PanelViewModel> {
  readonly #inbox: Inbox;
  readonly #source: NotificationsSource;
  readonly #clock: Clock;
  readonly #inboxHref: string;
  #state: State;
  #abort: AbortController | null = null;
  /** The changes streamed while a read is in flight, or null. */
  #inFlight: (readonly Notification[])[] | null = null;
  #focusId = 0;

  /** `seed` is the page the server rendered the bell with, or null. */
  constructor(inbox: Inbox, source: NotificationsSource, clock: Clock, inboxHref: string, seed: NotificationPage | null) {
    const state: State = {
      items: seed ? seed.notifications.slice(0, PANEL_LIMIT) : [],
      loaded: seed !== null,
      openLoaded: false,
      loading: false,
      unreadOnly: false,
      open: false,
      error: null,
      now: null,
      unreadCount: inbox.state.unreadCount,
      feed: inbox.state.feed,
      focus: null,
    };
    super(viewModel(state, inboxHref));
    this.#state = state;
    this.#inbox = inbox;
    this.#source = source;
    this.#clock = clock;
    this.#inboxHref = inboxHref;
    inbox.subscribe((s) => this.#change({ unreadCount: s.unreadCount, feed: s.feed }));
    inbox.watch({
      apply: (changed) => {
        // Changes that stream while a read is in flight may be missing from its answer: keep them to merge over it.
        this.#inFlight?.push(changed);
        this.#change({ items: openFirst(merge(this.#state.items, changed, { unread: this.#state.unreadOnly })).slice(0, PANEL_LIMIT) });
      },
      readAll: (at) => {
        const before = new Map(this.#state.items.map((n) => [n.id, n]));
        this.#change({ items: this.#state.items.map((n) => markedRead(n, at)) });
        // Puts back the rows this read marked, and only those: the stream may have changed others since.
        return () => this.#change({ items: this.#state.items.map((n) => (n.readAt === at && before.has(n.id) ? before.get(n.id)! : n)) });
      },
      reload: () => {
        if (this.#state.loaded) void this.#load();
      },
    });
  }

  toggle = (): void => {
    if (this.#state.open) this.close(true);
    else this.#show();
  };

  /** Closes the panel; `refocus` gives the focus back to the bell. */
  close = (refocus = false): void => {
    if (!this.#state.open) return;
    this.#change({ open: false, ...(refocus ? { focus: this.#focus("bell") } : {}) });
  };

  /** Escape closes the panel and gives the focus back to the bell. */
  escape = (): void => this.close(true);

  /** Focus moved to `outside` (true: away from both the bell and the panel): the panel closes. */
  focusMoved = (outside: boolean): void => {
    if (outside) this.close(false);
  };

  showAll = (): void => this.#filterTo(false);
  showUnread = (): void => this.#filterTo(true);

  markAll = async (): Promise<void> => {
    this.#change({ error: null });
    try {
      await this.#inbox.markAllRead();
    } catch (err) {
      this.#change({ error: inboxErrorMessage(err) });
    }
  };

  markOne = async (id: string): Promise<void> => {
    const n = this.#state.items.find((item) => item.id === id);
    if (!n) return;
    this.#change({ error: null });
    try {
      await this.#inbox.markRead([n]);
    } catch (err) {
      this.#change({ error: inboxErrorMessage(err) });
    }
  };

  /** Opens a notification at its link and marks it read; `keepPage` (a new tab) only marks it. */
  openItem = async (id: string, keepPage = false): Promise<void> => {
    const n = this.#state.items.find((item) => item.id === id);
    if (n) await this.#inbox.open(n, { keepPage });
  };

  dismissError = (): void => this.#change({ error: null });

  /** Reload: the panel reads again. */
  retry = (): void => void this.#load();

  /** The ages are read from the clock: once the view has mounted, and again while the panel is open. */
  tick = (): void => this.#change({ now: new Date(this.#clock.now()) });

  #show(): void {
    this.#change({ open: true, now: new Date(this.#clock.now()), focus: this.#focus("panel") });
    if (!this.#state.loaded || !this.#state.openLoaded) void this.#load();
  }

  #filterTo(unreadOnly: boolean): void {
    if (this.#state.unreadOnly === unreadOnly) return;
    this.#change({ unreadOnly });
    void this.#load();
  }

  async #load(): Promise<void> {
    this.#abort?.abort();
    const abort = new AbortController();
    this.#abort = abort;
    const since = this.#inbox.stamp;
    this.#change({ loading: true, error: null });
    const buffered: (readonly Notification[])[] = [];
    this.#inFlight = buffered;
    try {
      // The latest page, and the open causes however old: Needs you leads the panel.
      const { latest, open } = await loadPanel(this.#source, this.#state.unreadOnly, abort.signal);
      if (this.#abort !== abort) return;
      this.#inbox.listed(latest, since);
      this.#inbox.track(open.notifications);
      this.#change({ items: openFirst(upsert(latest.notifications, open.notifications.filter(isOpen))).slice(0, PANEL_LIMIT), loaded: true, openLoaded: true, now: new Date(this.#clock.now()) });
      // Then the changes that streamed meanwhile, which the answer may predate.
      this.#inFlight = null;
      for (const changed of buffered) this.#apply(changed);
    } catch (err) {
      if (this.#abort === abort && !(err instanceof Error && err.name === "AbortError")) this.#change({ error: inboxErrorMessage(err) });
    } finally {
      // A read that a newer one replaced leaves the busy state to it.
      if (this.#abort === abort) {
        this.#inFlight = null;
        this.#change({ loading: false });
      }
    }
  }

  #apply(changed: readonly Notification[]): void {
    this.#change({ items: openFirst(merge(this.#state.items, changed, { unread: this.#state.unreadOnly })).slice(0, PANEL_LIMIT) });
  }

  #focus(target: FocusRequest["target"]): FocusRequest {
    return { target, id: ++this.#focusId };
  }

  #change(change: Partial<State>): void {
    this.#state = { ...this.#state, ...change };
    this.set(viewModel(this.#state, this.#inboxHref));
  }
}

function viewModel(state: State, inboxHref: string): PanelViewModel {
  const empty = state.loaded && state.items.length === 0;
  return {
    open: state.open,
    unreadOnly: state.unreadOnly,
    rows: openFirst(state.items).map((n) => toRow(n, state.now)),
    loading: state.loading,
    empty,
    emptyText: state.unreadOnly ? "Nothing unread." : "No notifications yet.",
    canMarkAll: !!state.unreadCount,
    inboxHref,
    error: state.error,
    streamNotice: streamNotice(state.feed),
    focus: state.focus,
  };
}
