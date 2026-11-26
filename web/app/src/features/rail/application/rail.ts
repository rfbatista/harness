// The rail as a person works it: the project's model (the seed, then the
// project feed) plus what the person did on it: the filter, Needs you, folded
// groups, opened features, the focused row, the phone drawer; and what the
// feed did to it: the rows that just arrived, the dots that ring, and the
// announcement of tasks that start waiting. The presenter maps this state to
// what the view shows.

import { needsYou, railGroups } from "../domain/rail-groups";
import { taskHref, type Task } from "../domain/task";
import { activityOf, followProject, type RailState, type RailUpdate } from "./follow-project";
import type { Announcer, FeaturePrefs, PageOpener, RailFeed, RailPrefs, RailSeed, RailSource, Scheduler, ServicesCount } from "./ports";

/** How long a task that arrived or moved wears the arrival wash. */
export const FRESH_MS = 8_000;
/** How long a task that started waiting rings its dot. */
export const SIGNAL_MS = 2_000;
/** Tasks that start waiting within this window are announced in one sentence. */
export const ANNOUNCE_MS = 2_000;
/** The filter's result is read out once typing stops this long. */
export const RESULT_MS = 500;

export interface RailModelState extends RailState {
  readonly projectId: string;
  /** The open task's id, marked current; "" when none. */
  readonly currentTaskId: string;
  /** The project page open now (a PageKey), marked current in the navigation; "" for any other page. */
  readonly page: string;
  readonly query: string;
  /** Read out by the polite region under the filter. */
  readonly filterResult: string;
  readonly needsYouOnly: boolean;
  /** The folded groups' keys. */
  readonly collapsed: readonly string[];
  /** A heading was clicked: from now on the chevron turns (not on the first paint). */
  readonly turned: boolean;
  readonly freshIds: readonly string[];
  readonly signalIds: readonly string[];
  /** The task row with focus: it peeks out of a collapsed group. */
  readonly focusedId: string;
  /** The feature rows the person opened or folded on this page; the rest read the browser's preference. */
  readonly opened: Readonly<Record<string, boolean>>;
  /** The phone drawer is open. */
  readonly drawerOpen: boolean;
  /** Services starting or running: the Services item's badge, kept current by the services feed. */
  readonly servicesRunning: number;
  /** The browser's own preferences were read: until then the rail is what the server painted (every feature folded). */
  readonly prefsRead: boolean;
}

export interface RailInput {
  readonly source: RailSource;
  readonly feed: RailFeed;
  readonly seed: RailSeed;
  readonly currentTaskId: string;
  readonly page?: string;
  /** The folded groups at first paint: what the server drew. */
  readonly collapsed: readonly string[];
  readonly prefs: RailPrefs;
  readonly features: FeaturePrefs;
  readonly announcer: Announcer;
  readonly opener: PageOpener;
  readonly scheduler: Scheduler;
  /** The project's running services, followed for the Services item's badge; absent when the item is not served. */
  readonly services?: ServicesCount;
  /** What the server counted: the badge until the services feed says otherwise. */
  readonly servicesRunning?: number;
}

/** How many tasks need the person: a session waits, or a review does. */
export const needsYouCount = (state: RailState): number => {
  const byTask = activityOf(state);
  return state.tasks.filter((t) => !t.archivedAt && needsYou(t, byTask[t.id])).length;
};

/** Review requests waiting on the person across the project. */
export const reviewsCount = (state: RailState): number => state.tasks.reduce((n, t) => n + (t.archivedAt ? 0 : t.pendingReviews), 0);

/** A feature row is open: what the person chose on this page, else the browser's preference. */
export const isFeatureOpen = (state: RailModelState, features: FeaturePrefs, id: string): boolean => (id in state.opened ? state.opened[id] === true : state.prefsRead && features.isOpen(id));

export class Rail {
  #state: RailModelState;
  readonly #listeners = new Set<(state: RailModelState) => void>();
  readonly #input: RailInput;
  readonly #stopFollowing: () => void;
  #stopServices: (() => void) | null = null;
  readonly #timers = new Set<() => void>();
  #announcing: string[] = [];
  #announceTimer: (() => void) | null = null;
  #resultTimer: (() => void) | null = null;

  constructor(input: RailInput) {
    this.#input = input;
    const follow = followProject({ source: input.source, feed: input.feed, seed: input.seed, onUpdate: (u) => this.#followed(u), });
    this.#state = {
      ...follow.state(),
      projectId: input.seed.projectId,
      currentTaskId: input.currentTaskId,
      page: input.page ?? "",
      query: "",
      filterResult: "",
      needsYouOnly: false,
      collapsed: [...new Set(input.collapsed)],
      turned: false,
      freshIds: [],
      signalIds: [],
      focusedId: "",
      opened: {},
      drawerOpen: false,
      prefsRead: false,
      servicesRunning: input.servicesRunning ?? 0,
    };
    this.#stopFollowing = follow.stop;
    if (input.services) this.#followServices(input.services, input.seed.projectId);
  }

  get state(): RailModelState {
    return this.#state;
  }

  subscribe = (listener: (state: RailModelState) => void): (() => void) => {
    this.#listeners.add(listener);
    listener(this.#state);
    return () => void this.#listeners.delete(listener);
  };

  #set(change: Partial<RailModelState>): void {
    this.#state = { ...this.#state, ...change };
    for (const l of [...this.#listeners]) l(this.#state);
  }

  /** A timer the model owns, dropped by stop(). */
  #later(ms: number, run: () => void): () => void {
    const cancel = this.#input.scheduler.after(ms, () => {
      this.#timers.delete(cancel);
      run();
    });
    this.#timers.add(cancel);
    return () => {
      this.#timers.delete(cancel);
      cancel();
    };
  }

  /** The services feed carries the running count with each event; the list is read again after a drop. */
  #followServices(services: ServicesCount, projectId: string): void {
    let seq = 0;
    const show = (running: number): void => this.#set({ servicesRunning: running });
    const stop = services.follow(projectId, {
      onCount: (count) => {
        if (count.seq <= seq) return;
        seq = count.seq;
        show(count.running);
      },
      onStatus: (status) => {
        if (status !== "resynced") return;
        // Events were missed: the list says how many run now, and the sequence it is current to.
        void services.read(projectId).then(
          (list) => {
            seq = Math.max(seq, list.seq);
            show(list.running);
          },
          () => {}, // the next event or resync corrects it
        );
      },
    });
    this.#stopServices = stop;
  }

  #followed(update: RailUpdate): void {
    const { tasks, sessions, archivedCount } = update.state;
    this.#set({ tasks, sessions, archivedCount });
    if (update.cause !== "change") return; // a resync is not news: no wash, no announcement
    const change = update.change;
    if (change?.kind === "task-upsert") {
      const previous = update.before.tasks.find((t) => t.id === change.task.id);
      if (!previous || previous.status !== change.task.status) this.#markFresh(change.task.id);
    }
    this.#noteWaiting(update.before, update.state);
  }

  /** Tasks that started waiting: their dot rings once, and the stream bar says so. */
  #noteWaiting(before: RailState, after: RailState): void {
    const waiting = (s: RailState): Set<string> => {
      const byTask = activityOf(s);
      return new Set(s.tasks.filter((t) => needsYou(t, byTask[t.id])).map((t) => t.id));
    };
    const was = waiting(before);
    const byId = new Map(after.tasks.map((t) => [t.id, t] as const));
    const started = [...waiting(after)].filter((id) => !was.has(id)).map((id) => byId.get(id)!);
    for (const t of started) {
      this.#set({ signalIds: [...this.#state.signalIds, t.id] });
      this.#later(SIGNAL_MS, () => this.#set({ signalIds: this.#state.signalIds.filter((x) => x !== t.id) }));
      this.#announcing.push(t.title);
    }
    if (this.#announcing.length > 0 && this.#announceTimer === null) this.#announceTimer = this.#later(ANNOUNCE_MS, () => this.#announce());
  }

  #announce(): void {
    const titles = this.#announcing;
    this.#announcing = [];
    this.#announceTimer = null;
    this.#input.announcer.announce(titles.length === 1 ? `“${titles[0]}” is waiting on you.` : `${titles.length} tasks are waiting on you.`);
  }

  #markFresh(id: string): void {
    this.#set({ freshIds: [...this.#state.freshIds.filter((x) => x !== id), id] });
    this.#later(FRESH_MS, () => this.#set({ freshIds: this.#state.freshIds.filter((x) => x !== id) }));
  }

  /** The rows the filter and Needs you keep, in order: what Enter opens the first of. */
  matchedTasks(): Task[] {
    const s = this.#state;
    return railGroups(s.tasks, { byTask: activityOf(s), currentId: s.currentTaskId, collapsed: s.collapsed, query: s.query, needsYouOnly: s.needsYouOnly }).flatMap((g) => g.rows);
  }

  // ── Folding, the filter, Needs you ───────────────────────────────────

  /** A heading's button: fold or unfold its group, and remember it. */
  toggleGroup(key: string): void {
    const folding = !this.#state.collapsed.includes(key);
    const collapsed = folding ? [...this.#state.collapsed, key] : this.#state.collapsed.filter((k) => k !== key);
    this.#input.prefs.setCollapsed(collapsed);
    this.#set({ turned: true, collapsed });
  }

  /** Folding the group a row has focus in (only possible with the mouse) moves focus to its heading: the row no longer peeks. */
  clearFocusedRow(): void {
    this.#set({ focusedId: "" });
  }

  /** A feature row's twisty: shows or hides its children, and remembers it in this browser. */
  toggleFeature(id: string): void {
    const open = !isFeatureOpen(this.#state, this.#input.features, id);
    this.#set({ opened: { ...this.#state.opened, [id]: open } });
    this.#input.features.setOpen(id, open);
  }

  toggleNeedsYou(): void {
    this.#set({ needsYouOnly: !this.#state.needsYouOnly });
  }

  /** The filter's text changed: say how many match once typing stops. */
  setQuery(query: string): void {
    this.#set({ query });
    this.#sayResult();
  }

  #sayResult(): void {
    this.#resultTimer?.();
    this.#resultTimer = this.#later(RESULT_MS, () => {
      this.#resultTimer = null;
      const n = this.matchedTasks().length;
      let text: string;
      if (this.#state.query.trim() === "") text = "";
      else if (n === 0) text = "No task matches";
      else text = n === 1 ? "1 task matches" : `${n} tasks match`;
      this.#set({ filterResult: text });
    });
  }

  clearQuery(): void {
    this.#resultTimer?.();
    this.#resultTimer = null;
    this.#set({ query: "", filterResult: "" });
  }

  /** Enter in the filter: open the first match. False when there is no text or no match. */
  openFirstMatch(): boolean {
    if (this.#state.query.trim() === "") return false;
    const first = this.matchedTasks()[0];
    if (!first) return false;
    this.#input.opener.open(taskHref(this.#state.projectId, first.id));
    return true;
  }

  // ── Focus, navigation, the drawer ────────────────────────────────────

  setFocused(taskId: string): void {
    if (taskId !== this.#state.focusedId) this.#set({ focusedId: taskId });
  }

  /** In-place navigation: the task that is open changed. */
  setCurrentTask(taskId: string): void {
    if (taskId !== this.#state.currentTaskId) this.#set({ currentTaskId: taskId });
  }

  /** In-place navigation: the project page that is open changed. */
  setPage(page: string): void {
    if (page !== this.#state.page) this.#set({ page });
  }

  /** The island took over the server's paint: the browser's preferences (opened features) now apply. */
  readPrefs(): void {
    if (!this.#state.prefsRead) this.#set({ prefsRead: true });
  }

  openDrawer(): void {
    if (!this.#state.drawerOpen) this.#set({ drawerOpen: true });
  }

  closeDrawer(): void {
    if (this.#state.drawerOpen) this.#set({ drawerOpen: false });
  }

  stop(): void {
    this.#stopFollowing();
    this.#stopServices?.();
    for (const cancel of [...this.#timers]) cancel();
    this.#timers.clear();
    this.#listeners.clear();
  }
}
