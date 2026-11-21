// The panel behind the bell: the latest notifications across every project,
// All or Unread, opened one at a time, marked read one by one or all at once.
// It is one of the inbox's views: the stream's changes reach it there.

import type { Clock } from "../../../../shared/kernel/clock";
import { Presenter } from "../../../../shared/kernel/presenter";
import { Inbox, PANEL_LIMIT } from "../../application/inbox";
import { loadPanel } from "../../application/load-panel";
import type { NotificationsSource } from "../../application/ports";
import { markedRead, merge, type Notification, type NotificationPage } from "../../domain/notification";
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
  /** A failed action (mark all read), shown with a Dismiss button. */
  readonly error: string | null;
  readonly focus: FocusRequest | null;
}

interface State {
  items: readonly Notification[];
  loaded: boolean;
  loading: boolean;
  unreadOnly: boolean;
  open: boolean;
  error: string | null;
  /** Null until the view has mounted: ages are not part of the server render, which could not match them. */
  now: Date | null;
  unreadCount: number | null;
  focus: FocusRequest | null;
}

export class PanelPresenter extends Presenter<PanelViewModel> {
  readonly #inbox: Inbox;
  readonly #source: NotificationsSource;
  readonly #clock: Clock;
  readonly #inboxHref: string;
  #state: State;
  #abort: AbortController | null = null;
  #focusId = 0;

  /** `seed` is the page the server rendered the bell with, or null. */
  constructor(inbox: Inbox, source: NotificationsSource, clock: Clock, inboxHref: string, seed: NotificationPage | null) {
    const state: State = {
      items: seed ? seed.notifications.slice(0, PANEL_LIMIT) : [],
      loaded: seed !== null,
      loading: false,
      unreadOnly: false,
      open: false,
      error: null,
      now: null,
      unreadCount: inbox.state.unreadCount,
      focus: null,
    };
    super(viewModel(state, inboxHref));
    this.#state = state;
    this.#inbox = inbox;
    this.#source = source;
    this.#clock = clock;
    this.#inboxHref = inboxHref;
    inbox.subscribe((s) => this.#change({ unreadCount: s.unreadCount }));
    inbox.watch({
      apply: (changed) => this.#change({ items: merge(this.#state.items, changed, { unread: this.#state.unreadOnly }).slice(0, PANEL_LIMIT) }),
      readAll: (at) => this.#change({ items: this.#state.items.map((n) => markedRead(n, at)) }),
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
      this.#change({ error: describe(err) });
    }
  };

  markOne = async (id: string): Promise<void> => {
    const n = this.#state.items.find((item) => item.id === id);
    if (!n) return;
    this.#change({ error: null });
    try {
      await this.#inbox.markRead([n]);
    } catch (err) {
      this.#change({ error: describe(err) });
    }
  };

  /** Opens a notification at its link and marks it read; `keepPage` (a new tab) only marks it. */
  openItem = async (id: string, keepPage = false): Promise<void> => {
    const n = this.#state.items.find((item) => item.id === id);
    if (n) await this.#inbox.open(n, { keepPage });
  };

  dismissError = (): void => this.#change({ error: null });

  /** The ages are read from the clock: once the view has mounted, and again while the panel is open. */
  tick = (): void => this.#change({ now: new Date(this.#clock.now()) });

  #show(): void {
    this.#change({ open: true, now: new Date(this.#clock.now()), focus: this.#focus("panel") });
    if (!this.#state.loaded) void this.#load();
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
    try {
      const page = await loadPanel(this.#source, this.#state.unreadOnly, abort.signal);
      if (this.#abort !== abort) return;
      this.#inbox.listed(page, since);
      this.#change({ items: page.notifications, loaded: true, now: new Date(this.#clock.now()) });
    } catch (err) {
      if (this.#abort === abort && !(err instanceof Error && err.name === "AbortError")) this.#change({ error: describe(err) });
    } finally {
      if (this.#abort === abort) this.#change({ loading: false });
    }
  }

  #focus(target: FocusRequest["target"]): FocusRequest {
    return { target, id: ++this.#focusId };
  }

  #change(change: Partial<State>): void {
    this.#state = { ...this.#state, ...change };
    this.set(viewModel(this.#state, this.#inboxHref));
  }
}

function describe(err: unknown): string {
  return err instanceof Error && err.message !== "" ? err.message : "Something went wrong.";
}

function viewModel(state: State, inboxHref: string): PanelViewModel {
  const empty = state.loaded && state.items.length === 0;
  return {
    open: state.open,
    unreadOnly: state.unreadOnly,
    rows: state.items.map((n) => toRow(n, state.now)),
    loading: state.loading,
    empty,
    emptyText: state.unreadOnly ? "Nothing unread." : "No notifications yet.",
    canMarkAll: !!state.unreadCount,
    inboxHref,
    error: state.error,
    focus: state.focus,
  };
}
