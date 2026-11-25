// The product owner in memory, for tests and for development without a server: it holds
// the settings and the threads, keeps the contract's rules (busy, off, invalid input) and
// lets a test play the server's side of a turn.

import { ProductOwnerError, type ProductOwnerEvents, type ProductOwnerGateway } from "../application/ports";
import { DIGEST_PAGE_SIZE, type Digest, type DigestPage } from "../domain/digest";
import type { FeedStatus } from "../domain/feed-status";
import { MAX_ENTRY, entryLength, type MemoryEntry, type MessageItem, type ProductOwnerEvent, type Settings, type ThreadHead, type ThreadItem, type ThreadPage, type ThreadSummary, type Turn } from "../domain/thread";

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
  /** The server's next digest is at `at` (null for none), whatever the digest time says: the scheduler moved on. Followers hear nothing, as the contract says. */
  setNextDigest(projectId: string, at: Date | null): void;
  /** The server holds this digest, without telling anyone. */
  seedDigest(projectId: string, digest: Digest): void;
  /** The server recorded this digest, for its date: it replaces the one of that date and tells every follower of the project. */
  recordDigest(projectId: string, digest: Digest): void;
  /** The server holds this thread of `projectId`, with these items. */
  seedThread(projectId: string, thread: Partial<ThreadSummary> & { threadId: string }, items?: readonly ThreadItem[], sessionId?: string): void;
  /** The server holds these entries of `projectId` (no event is sent). */
  seedMemory(projectId: string, entries: readonly MemoryEntry[]): void;
  /** The product owner saved (or changed) this entry: the server holds it and tells the followers. */
  remember(projectId: string, entry: MemoryEntry, op: "saved" | "changed"): void;
  /** The product owner dropped this entry. */
  forget(entryId: string): void;
  /** The server did this: it changes what it holds and tells every follower of the thread's project. */
  publish(projectId: string, event: ProductOwnerEvent): void;
  /** The thread's turn changes (and the thread event follows). */
  setTurn(threadId: string, turn: Turn): void;
  /** The stream's state changes. */
  setStatus(status: FeedStatus): void;
  /** Every next call fails with `error` until `null`. */
  failWith(error: ProductOwnerError | null): void;
  /** While silent, the server does not send the echo of the person's message on the stream. */
  silenceEchoes(silent: boolean): void;
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
  const digests = new Map<string, Digest[]>();
  const followers = new Set<{ projectId: string; onEvent: (e: ProductOwnerEvent) => void; onStatus: (s: FeedStatus) => void }>();
  const stops: string[] = [];
  const memories = new Map<string, MemoryEntry[]>();
  const entryProject = (entryId: string): { projectId: string; entries: MemoryEntry[] } => {
    for (const [projectId, entries] of memories) if (entries.some((e) => e.entryId === entryId)) return { projectId, entries };
    throw new ProductOwnerError("MEMORY_ENTRY_NOT_FOUND", `no memory entry ${entryId}`);
  };
  const newestFirst = (a: MemoryEntry, b: MemoryEntry): number => b.savedAt.getTime() - a.savedAt.getTime();
  let status: FeedStatus = "live";
  let failure: ProductOwnerError | null = null;
  let hold: Promise<void> | null = null;
  let silent = false;
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
  /** What the server computes: the next digest time after "now" (2026-11-24 07:00 at -03:00), none while no time is set or the owner is off. */
  const withNext = (s: Settings): Settings => {
    if (!s.enabled || s.digestTime === null) return { ...s, nextDigestAt: null };
    const today = new Date(`2026-11-24T${s.digestTime}:00-03:00`);
    return { ...s, nextDigestAt: today.getTime() > Date.parse("2026-11-24T10:00:00Z") ? today : new Date(today.getTime() + 24 * 3600 * 1000) };
  };
  /** A project has at most one digest per date. */
  const keep = (projectId: string, digest: Digest): void => {
    const held = (digests.get(projectId) ?? []).filter((d) => d.date !== digest.date);
    digests.set(projectId, [...held, digest].sort((a, b) => b.date.localeCompare(a.date)));
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
    if (!silent) send(t.projectId, { type: "item", threadId: t.summary.threadId, item });
    return item;
  };
  const late = async (): Promise<void> => {
    const wait = hold;
    hold = null;
    if (wait) await wait;
  };

  return {
    seedProject: (projectId, enabled) => void settings.set(projectId, { projectId, enabled, digestTime: null, timeZone: "America/Sao_Paulo", nextDigestAt: null }),
    setNextDigest(projectId, at) {
      settings.set(projectId, { ...known(projectId), nextDigestAt: at });
    },
    seedDigest(projectId, digest) {
      known(projectId);
      keep(projectId, digest);
    },
    recordDigest(projectId, digest) {
      known(projectId);
      keep(projectId, digest);
      send(projectId, { type: "digest", digestId: digest.digestId, date: digest.date });
    },
    seedMemory: (projectId, entries) => void memories.set(projectId, [...entries]),
    remember(projectId, entry, op) {
      const entries = (memories.get(projectId) ?? []).filter((e) => e.entryId !== entry.entryId);
      memories.set(projectId, [...entries, entry]);
      send(projectId, { type: "memory", entryId: entry.entryId, op });
    },
    forget(entryId) {
      const { projectId, entries } = entryProject(entryId);
      memories.set(projectId, entries.filter((e) => e.entryId !== entryId));
      send(projectId, { type: "memory", entryId, op: "dropped" });
    },
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
      } else if (event.type === "settings") settings.set(projectId, event.settings);
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
    silenceEchoes: (value) => void (silent = value),
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
      const updated: Settings = withNext({ ...known(projectId), enabled });
      settings.set(projectId, updated);
      send(projectId, { type: "settings", settings: updated });
      return updated;
    },
    async updateSettings(projectId, patch) {
      check();
      let updated = known(projectId);
      if (patch.digestTime !== undefined) {
        if (patch.digestTime !== "" && !/^([01]\d|2[0-3]):[0-5]\d$/.test(patch.digestTime)) throw new ProductOwnerError("INVALID_INPUT", "the digest time is not an HH:MM time");
        updated = withNext({ ...updated, digestTime: patch.digestTime === "" ? null : patch.digestTime });
      }
      settings.set(projectId, updated);
      send(projectId, { type: "settings", settings: updated });
      return updated;
    },
    async digests(projectId, before, signal): Promise<DigestPage> {
      signal?.throwIfAborted();
      check();
      known(projectId);
      const older = (digests.get(projectId) ?? []).filter((d) => before === undefined || d.date < before);
      return { digests: older.slice(0, DIGEST_PAGE_SIZE), hasEarlier: older.length > DIGEST_PAGE_SIZE };
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
    async memory(projectId, signal) {
      signal?.throwIfAborted();
      check();
      known(projectId);
      return [...(memories.get(projectId) ?? [])].sort(newestFirst);
    },
    async updateMemoryEntry(entryId, text) {
      check();
      const { projectId, entries } = entryProject(entryId);
      if (text.trim() === "" || entryLength(text) > MAX_ENTRY) throw new ProductOwnerError("INVALID_INPUT", "the entry is empty or too long");
      const entry: MemoryEntry = { ...entries.find((e) => e.entryId === entryId)!, text, changedAt: now(), changedBy: "person" };
      memories.set(projectId, entries.map((e) => (e.entryId === entryId ? entry : e)));
      send(projectId, { type: "memory", entryId, op: "changed" });
      return entry;
    },
    async deleteMemoryEntry(entryId) {
      check();
      const { projectId, entries } = entryProject(entryId);
      memories.set(projectId, entries.filter((e) => e.entryId !== entryId));
      send(projectId, { type: "memory", entryId, op: "dropped" });
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
