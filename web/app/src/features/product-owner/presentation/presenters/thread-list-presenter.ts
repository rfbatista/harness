// The thread list: the project's threads grouped by day, newest first, filtered by what the
// person types. A popup in the panel, a side column on the page.

import type { Clock } from "../../../../shared/kernel/clock";
import { Presenter } from "../../../../shared/kernel/presenter";
import type { Conversation } from "../../application/conversation";
import { threadHref } from "../../application/links";
import type { ThreadList, ThreadListState } from "../../application/thread-list";
import { isAnswering, type ThreadSummary } from "../../domain/thread";

export interface ThreadRowViewModel {
  readonly threadId: string;
  readonly title: string;
  readonly time: string;
  /** The first line of the last message. */
  readonly excerpt: string;
  /** The thread is answering now: a live dot. */
  readonly live: boolean;
  /** The thread on screen. */
  readonly current: boolean;
  readonly href: string;
}

export interface ThreadGroupViewModel {
  readonly label: string;
  readonly rows: readonly ThreadRowViewModel[];
}

export interface ThreadListViewModel {
  readonly query: string;
  readonly groups: readonly ThreadGroupViewModel[];
  /** What to say when there is nothing to list. */
  readonly empty: string | null;
  readonly loading: boolean;
  readonly error: string;
}

const MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];
const WEEKDAYS = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];
const DAY = 24 * 60 * 60 * 1000;

const dayStart = (d: Date): number => new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime();
const two = (n: number): string => String(n).padStart(2, "0");

/** "Today", "Yesterday", "Mon 23 Nov" (the year only when it is not this one). */
export function dayLabel(at: Date, now: Date): string {
  const days = Math.round((dayStart(now) - dayStart(at)) / DAY);
  if (days === 0) return "Today";
  if (days === 1) return "Yesterday";
  const base = `${WEEKDAYS[at.getDay()]} ${at.getDate()} ${MONTHS[at.getMonth()]}`;
  return at.getFullYear() === now.getFullYear() ? base : `${base} ${at.getFullYear()}`;
}

/** The clock time on today's rows, the date on older ones. */
export function rowTime(at: Date, now: Date): string {
  return dayStart(at) === dayStart(now) ? `${two(at.getHours())}:${two(at.getMinutes())}` : `${at.getDate()} ${MONTHS[at.getMonth()]}`;
}

export class ThreadListPresenter extends Presenter<ThreadListViewModel> {
  readonly #list: ThreadList;
  readonly #conversation: Conversation;
  readonly #clock: Clock;
  #query = "";
  #unsubscribe: Array<() => void> = [];

  constructor(list: ThreadList, conversation: Conversation, clock: Clock) {
    super({ query: "", groups: [], empty: null, loading: false, error: "" });
    this.#list = list;
    this.#conversation = conversation;
    this.#clock = clock;
    this.#unsubscribe = [list.subscribe(() => this.#rebuild()), conversation.subscribe(() => this.#rebuild())];
  }

  destroy(): void {
    for (const stop of this.#unsubscribe) stop();
    this.#unsubscribe = [];
  }

  setQuery = (query: string): void => {
    this.#query = query;
    this.#rebuild();
  };

  open = (threadId: string): void => this.#conversation.openThread(threadId);

  newThread = (): void => this.#conversation.newThread();

  #rebuild(): void {
    const state = this.#list.state;
    const projectId = this.#conversation.state.projectId;
    const current = this.#conversation.state.threadId;
    const now = new Date(this.#clock.now());
    const needle = this.#query.trim().toLowerCase();
    const shown = state.threads.filter((t) => needle === "" || t.title.toLowerCase().includes(needle) || t.lastMessageExcerpt.toLowerCase().includes(needle));
    const groups: { label: string; rows: ThreadRowViewModel[] }[] = [];
    for (const t of shown) {
      const label = dayLabel(t.lastActivityAt, now);
      let group = groups.at(-1);
      if (group?.label !== label) groups.push((group = { label, rows: [] }));
      group.rows.push(toRow(t, projectId, current, now));
    }
    this.set({ query: this.#query, groups, empty: emptyText(state, shown.length, needle), loading: state.loading, error: state.error });
  }
}

const toRow = (t: ThreadSummary, projectId: string, current: string | null, now: Date): ThreadRowViewModel => ({
  threadId: t.threadId,
  title: t.title,
  time: rowTime(t.lastActivityAt, now),
  excerpt: t.lastMessageExcerpt,
  live: isAnswering(t.turn),
  current: t.threadId === current,
  href: threadHref(projectId, t.threadId),
});

function emptyText(state: ThreadListState, shown: number, needle: string): string | null {
  if (shown > 0 || state.loading || state.error !== "") return null;
  return needle === "" ? "No threads yet. Ask the product owner something." : "No thread matches.";
}
