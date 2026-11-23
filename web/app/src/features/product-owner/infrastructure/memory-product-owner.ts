// The product owner in memory, for tests and for development without a server: it holds
// the settings and the threads, keeps the contract's rules (busy, off, invalid input) and
// lets a test play the server's side of a turn.

import { ProductOwnerError, type ProductOwnerEvents, type ProductOwnerGateway } from "../application/ports";
import type { FeedStatus } from "../domain/feed-status";
import type { MessageItem, ProductOwnerEvent, Settings, ThreadHead, ThreadItem, ThreadPage, ThreadSummary, Turn } from "../domain/thread";

export const PAGE_SIZE = 100;
export const MAX_MESSAGE = 20000;
export const MAX_TITLE = 200;

interface Stored {
  summary: ThreadSummary;
  projectId: string;
  sessionId: string;
  items: ThreadItem[];
}

export interface MemoryProductOwner extends ProductOwnerGateway, ProductOwnerEvents {
  /** The server holds this project, with its product owner on or off. */
  seedProject(projectId: string, enabled: boolean): void;
  /** The server holds this thread of `projectId`, with these items. */
  seedThread(projectId: string, thread: Partial<ThreadSummary> & { threadId: string }, items?: readonly ThreadItem[], sessionId?: string): void;
  /** The server did this: it changes what it holds and tells every follower of the thread's project. */
  publish(projectId: string, event: ProductOwnerEvent): void;
  /** The thread's turn changes (and the thread event follows). */
  setTurn(threadId: string, turn: Turn): void;
  /** The stream's state changes. */
  setStatus(status: FeedStatus): void;
  /** Every next call fails with `error` until `null`. */
  failWith(error: ProductOwnerError | null): void;
  /** The next send or start answers late: the call returns when the returned function is called. */
  holdNextSend(): () => void;
  readonly followers: number;
  readonly stops: readonly string[];
  /** What the server holds of `threadId`, oldest first. */
  itemsOf(threadId: string): readonly ThreadItem[];
}

const upsert = (items: ThreadItem[], item: ThreadItem): void => {
  const at = items.findIndex((i) => i.itemId === item.itemId);
  if (at >= 0) items[at] = item;
  else items.push(item);
};

export function memoryProductOwner(): MemoryProductOwner {
  const settings = new Map<string, Settings>();
  const threads = new Map<string, Stored>();
  const followers = new Set<{ projectId: string; onEvent: (e: ProductOwnerEvent) => void; onStatus: (s: FeedStatus) => void }>();
  const stops: string[] = [];
  let status: FeedStatus = "live";
  let failure: ProductOwnerError | null = null;
  let hold: Promise<void> | null = null;
  let counter = 0;
  const next = (prefix: string): string => `${prefix}${++counter}`;
  const now = (): Date => new Date(Date.UTC(2026, 10, 23, 12, 0, counter));

  const send = (projectId: string, event: ProductOwnerEvent): void => {
    for (const f of [...followers]) if (f.projectId === projectId) f.onEvent(event);
  };
  const find = (threadId: string): Stored => {
    const t = threads.get(threadId);
    if (!t) throw new ProductOwnerError("THREAD_NOT_FOUND", `no thread ${threadId}`);
    return t;
  };
  const check = (): void => {
    if (failure) throw failure;
  };
  const known = (projectId: string): Settings => {
    const s = settings.get(projectId);
    if (!s) throw new ProductOwnerError("PROJECT_NOT_FOUND", `no project ${projectId}`);
    return s;
  };
  const validate = (message: string): void => {
    if (message.trim() === "" || message.length > MAX_MESSAGE) throw new ProductOwnerError("INVALID_INPUT", "the message is empty or too long");
  };
  const touch = (t: Stored, patch: Partial<ThreadSummary>): void => {
    t.summary = { ...t.summary, ...patch };
    send(t.projectId, { type: "thread", thread: t.summary });
  };
  const personMessage = (t: Stored, text: string): MessageItem => {
    const item: MessageItem = { kind: "message", itemId: next("i"), at: now(), author: "person", text, streaming: false, stopped: false };
    t.items.push(item);
    touch(t, { lastActivityAt: item.at, lastMessageExcerpt: text.split("\n")[0]!.slice(0, 160), turn: "resuming" });
    send(t.projectId, { type: "item", threadId: t.summary.threadId, item });
    return item;
  };
  const late = async (): Promise<void> => {
    const wait = hold;
    hold = null;
    if (wait) await wait;
  };

  return {
    seedProject: (projectId, enabled) => void settings.set(projectId, { projectId, enabled }),
    seedThread(projectId, thread, items = [], sessionId = `s-${thread.threadId}`) {
      const at = new Date(Date.UTC(2026, 10, 23, 9, 0, 0));
      const summary: ThreadSummary = { title: thread.threadId, startedAt: at, lastActivityAt: at, lastMessageExcerpt: "", titleSetByPerson: false, turn: "idle", pendingHeldActions: 0, ...thread };
      threads.set(thread.threadId, { summary, projectId, sessionId, items: [...items] });
    },
    publish(projectId, event) {
      if (event.type === "item") {
        const t = threads.get(event.threadId);
        if (t) upsert(t.items, event.item);
      } else if (event.type === "delta") {
        const t = threads.get(event.threadId);
        const item = t?.items.find((i) => i.itemId === event.itemId);
        if (t && item?.kind === "message") upsert(t.items, { ...item, text: item.text + event.text, streaming: true });
      } else if (event.type === "thread") {
        const t = threads.get(event.thread.threadId);
        if (t) t.summary = event.thread;
      } else settings.set(projectId, event.settings);
      send(projectId, event);
    },
    setTurn(threadId, turn) {
      const t = find(threadId);
      touch(t, { turn });
    },
    setStatus(next) {
      status = next;
      for (const f of [...followers]) f.onStatus(next);
    },
    failWith: (error) => void (failure = error),
    holdNextSend() {
      let release = (): void => {};
      hold = new Promise<void>((resolve) => (release = resolve));
      return release;
    },
    get followers() {
      return followers.size;
    },
    get stops() {
      return stops;
    },
    itemsOf: (threadId) => find(threadId).items,

    follow(projectId, onEvent, onStatus) {
      const f = { projectId, onEvent, onStatus };
      followers.add(f);
      onStatus(status);
      return () => void followers.delete(f);
    },

    async settings(projectId, signal) {
      signal?.throwIfAborted();
      check();
      return known(projectId);
    },
    async setEnabled(projectId, enabled) {
      check();
      known(projectId);
      const updated: Settings = { projectId, enabled };
      settings.set(projectId, updated);
      send(projectId, { type: "settings", settings: updated });
      return updated;
    },
    async threads(projectId, signal) {
      signal?.throwIfAborted();
      check();
      known(projectId);
      return [...threads.values()].filter((t) => t.projectId === projectId).map((t) => t.summary).sort((a, b) => b.lastActivityAt.getTime() - a.lastActivityAt.getTime());
    },
    async thread(threadId, before, signal): Promise<ThreadPage> {
      signal?.throwIfAborted();
      check();
      const t = find(threadId);
      let end = t.items.length;
      if (before !== undefined) {
        end = t.items.findIndex((i) => i.itemId === before);
        if (end < 0) throw new ProductOwnerError("INVALID_INPUT", "before is not an item of the thread");
      }
      const start = Math.max(0, end - PAGE_SIZE);
      const head: ThreadHead = { threadId, projectId: t.projectId, title: t.summary.title, turn: t.summary.turn, sessionId: t.sessionId };
      return { head, items: t.items.slice(start, end), hasEarlier: start > 0 };
    },
    async startThread(projectId, message) {
      check();
      const s = known(projectId);
      if (!s.enabled) throw new ProductOwnerError("PRODUCT_OWNER_OFF", "the product owner is off");
      validate(message);
      const threadId = next("t");
      const at = now();
      const summary: ThreadSummary = { threadId, title: message.split("\n")[0]!.slice(0, 80), startedAt: at, lastActivityAt: at, lastMessageExcerpt: "", titleSetByPerson: false, turn: "idle", pendingHeldActions: 0 };
      const stored: Stored = { summary, projectId, sessionId: `s-${threadId}`, items: [] };
      threads.set(threadId, stored);
      const item = personMessage(stored, message);
      await late();
      return { threadId, itemId: item.itemId };
    },
    async sendMessage(threadId, message) {
      check();
      const t = find(threadId);
      if (!known(t.projectId).enabled) throw new ProductOwnerError("PRODUCT_OWNER_OFF", "the product owner is off");
      if (t.summary.turn !== "idle") throw new ProductOwnerError("THREAD_BUSY", "a turn of this thread is running");
      validate(message);
      const item = personMessage(t, message);
      await late();
      return { itemId: item.itemId };
    },
    async stopTurn(threadId) {
      check();
      find(threadId);
      stops.push(threadId);
    },
    async renameThread(threadId, title) {
      check();
      const t = find(threadId);
      if (title.trim() === "" || title.length > MAX_TITLE) throw new ProductOwnerError("INVALID_INPUT", "the title is empty or too long");
      touch(t, { title, titleSetByPerson: true });
      return t.summary;
    },
  };
}
