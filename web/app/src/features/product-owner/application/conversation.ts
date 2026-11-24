// One conversation with a project's product owner: the thread on screen, what the person is
// typing, and everything that arrives while they read. The panel and the page both show one.
//
// The person's message appears at once, "sending". The server's own echo of it can arrive on
// the stream before the send answers, so while a send is in flight the echo (a person's message
// of the same text) is taken for that message; otherwise a message is the server's by its id.
// Deltas show an answer as it arrives, and the final item that follows replaces them.

import type { FeedStatus } from "../domain/feed-status";
import type { ProductOwnerEvent, ThreadItem, Turn } from "../domain/thread";
import { ProductOwnerError, type ProductOwnerEvents, type ProductOwnerGateway } from "./ports";

/** A thread item as the conversation holds it. */
export type ConversationItem = ThreadItem & {
  /** The person's message the server has not answered for yet. */
  readonly sending?: boolean;
  /** It arrived from another tab of the same thread. */
  readonly fromOtherTab?: boolean;
};

export interface ConversationError {
  readonly code: string;
  readonly text: string;
  readonly fixUrl?: string;
}

export interface ConversationState {
  readonly projectId: string;
  /** null: a fresh thread that the first message starts. */
  readonly threadId: string | null;
  readonly title: string;
  readonly items: readonly ConversationItem[];
  readonly turn: Turn;
  readonly hasEarlier: boolean;
  readonly loading: boolean;
  /** A fresh thread with nothing in it: the starters show. */
  readonly fresh: boolean;
  readonly draft: string;
  readonly error: ConversationError | null;
  /** The thread's session, for the link to its Context tab. */
  readonly sessionId: string;
}

export interface ConversationDeps {
  readonly gateway: ProductOwnerGateway;
  readonly events: ProductOwnerEvents;
  /** The thread on screen changed (null: a fresh one): the browser remembers it. */
  readonly onThread?: (threadId: string | null) => void;
}

const EMPTY: ConversationState = { projectId: "", threadId: null, title: "", items: [], turn: "idle", hasEarlier: false, loading: false, fresh: true, draft: "", error: null, sessionId: "" };

export class Conversation {
  readonly #deps: ConversationDeps;
  readonly #listeners = new Set<(state: ConversationState) => void>();
  #state: ConversationState = EMPTY;
  #unfollow: (() => void) | null = null;
  /** The send in flight: its text, and the local id of the item that stands for it. */
  #inFlight: { text: string; localId: string } | null = null;
  #localIds = 0;
  /** What this tab sent: its ids are not "from another tab". */
  readonly #own = new Set<string>();
  #generation = 0;
  #loadingEarlier = false;

  constructor(deps: ConversationDeps) {
    this.#deps = deps;
  }

  get state(): ConversationState {
    return this.#state;
  }

  subscribe = (listener: (state: ConversationState) => void): (() => void) => {
    this.#listeners.add(listener);
    listener(this.#state);
    return () => void this.#listeners.delete(listener);
  };

  /** Starts following `projectId`'s product owner and shows `threadId` (null: a fresh thread). The returned function stops. */
  attach(projectId: string, threadId: string | null): () => void {
    this.#detach();
    const generation = ++this.#generation;
    this.#state = { ...EMPTY, projectId, draft: this.#state.projectId === projectId ? this.#state.draft : "" };
    this.#unfollow = this.#deps.events.follow(
      projectId,
      (event) => this.#onEvent(event),
      (status) => this.#onStatus(status),
    );
    void this.#open(threadId, generation);
    this.#emit();
    return () => {
      if (generation === this.#generation) this.#detach();
    };
  }

  setDraft(draft: string): void {
    this.#set({ draft });
  }

  /** Leaves the thread for a fresh one. */
  newThread(): void {
    this.#generation++;
    this.#inFlight = null;
    this.#set({ ...EMPTY, projectId: this.#state.projectId, draft: this.#state.draft });
    this.#deps.onThread?.(null);
  }

  /** Shows another thread of the project. */
  openThread(threadId: string): void {
    const generation = ++this.#generation;
    this.#inFlight = null;
    this.#set({ ...EMPTY, projectId: this.#state.projectId, draft: this.#state.draft, loading: true });
    void this.#open(threadId, generation);
  }

  /** Sends `text`, or the draft: the first message starts the thread, later ones resume it. */
  async send(text: string = this.#state.draft): Promise<void> {
    const message = text.trim();
    if (message === "" || this.#inFlight) return;
    const { projectId, threadId } = this.#state;
    const localId = `local-${++this.#localIds}`;
    this.#inFlight = { text: message, localId };
    const pending: ConversationItem = { kind: "message", itemId: localId, at: new Date(), author: "person", text: message, streaming: false, stopped: false, sending: true };
    this.#set({ items: [...this.#state.items, pending], draft: "", error: null, fresh: false });
    const generation = this.#generation;
    try {
      const sent = threadId === null ? await this.#deps.gateway.startThread(projectId, message) : { threadId, ...(await this.#deps.gateway.sendMessage(threadId, message)) };
      if (generation !== this.#generation) return;
      this.#own.add(sent.itemId);
      // The send answered: the item is the server's now. If its echo came first, it is already there.
      this.#inFlight = null;
      const has = this.#state.items.some((i) => i.itemId === sent.itemId);
      this.#set({
        threadId: sent.threadId,
        items: has ? this.#state.items.filter((i) => i.itemId !== localId) : this.#state.items.map((i) => (i.itemId === localId ? { ...i, itemId: sent.itemId, sending: false } : i)),
      });
      if (threadId === null) this.#deps.onThread?.(sent.threadId);
    } catch (err) {
      if (!(err instanceof ProductOwnerError)) throw err;
      if (generation !== this.#generation) return;
      this.#inFlight = null;
      // Nothing was stored: the text goes back to the box, and the person is told why.
      this.#set({
        items: this.#state.items.filter((i) => i.itemId !== localId),
        draft: message,
        fresh: this.#state.threadId === null && this.#state.items.length === 1,
        error: { code: err.code, text: err.message, ...(err.fixUrl === undefined ? {} : { fixUrl: err.fixUrl }) },
      });
    }
  }

  /** Stops the turn that is running. The partial answer stays in the thread. */
  async stop(): Promise<void> {
    const { threadId } = this.#state;
    if (threadId === null) return;
    try {
      await this.#deps.gateway.stopTurn(threadId);
    } catch (err) {
      if (!(err instanceof ProductOwnerError)) throw err;
      this.#set({ error: { code: err.code, text: err.message } });
    }
  }

  /** Reads the hundred items before the first one on screen. A second call while one is under way does nothing. */
  async loadEarlier(): Promise<void> {
    const { threadId, items, hasEarlier } = this.#state;
    const first = items.find((i) => !i.sending);
    if (threadId === null || !hasEarlier || this.#loadingEarlier || first === undefined) return;
    this.#loadingEarlier = true;
    const generation = this.#generation;
    try {
      const page = await this.#deps.gateway.thread(threadId, first.itemId);
      if (generation !== this.#generation) return;
      const known = new Set(this.#state.items.map((i) => i.itemId));
      this.#set({ items: [...page.items.filter((i) => !known.has(i.itemId)), ...this.#state.items], hasEarlier: page.hasEarlier });
    } catch (err) {
      if (!(err instanceof ProductOwnerError)) throw err;
      this.#set({ error: { code: err.code, text: err.message } });
    } finally {
      this.#loadingEarlier = false;
    }
  }

  /** Renames the thread as the person's. Resolves true when the server accepted the title; a refusal is shown and the title stays. */
  async rename(title: string): Promise<boolean> {
    const { threadId } = this.#state;
    if (threadId === null) return false;
    try {
      const renamed = await this.#deps.gateway.renameThread(threadId, title.trim());
      if (this.#state.threadId === threadId) this.#set({ title: renamed.title, error: null });
      return true;
    } catch (err) {
      if (!(err instanceof ProductOwnerError)) throw err;
      this.#set({ error: { code: err.code, text: err.message } });
      return false;
    }
  }

  dismissError(): void {
    this.#set({ error: null });
  }

  async #open(threadId: string | null, generation: number): Promise<void> {
    if (threadId === null) {
      this.#deps.onThread?.(null);
      return;
    }
    this.#set({ loading: true });
    try {
      const page = await this.#deps.gateway.thread(threadId);
      if (generation !== this.#generation) return;
      this.#set({ threadId, title: page.head.title, turn: page.head.turn, sessionId: page.head.sessionId, items: page.items, hasEarlier: page.hasEarlier, loading: false, fresh: false });
      this.#deps.onThread?.(threadId);
    } catch (err) {
      if (!(err instanceof ProductOwnerError)) throw err;
      if (generation !== this.#generation) return;
      // A thread that is gone (or cannot be read) leaves a fresh one, and says so when it was not gone.
      this.#set({ loading: false, fresh: true, threadId: null, error: err.code === "THREAD_NOT_FOUND" ? null : { code: err.code, text: err.message } });
      this.#deps.onThread?.(null);
    }
  }

  #onStatus(status: FeedStatus): void {
    // What was missed while the stream was down is read again.
    if (status === "resynced" && this.#state.threadId !== null) {
      const generation = this.#generation;
      const threadId = this.#state.threadId;
      void this.#deps.gateway
        .thread(threadId)
        .then((page) => {
          if (generation !== this.#generation) return;
          // A message still sending may already be stored: the page then holds it, and it is not shown twice.
          const known = new Set(this.#state.items.map((i) => i.itemId));
          const stored = (text: string) => page.items.some((i) => i.kind === "message" && i.author === "person" && i.text.trim() === text && !known.has(i.itemId));
          const sending = this.#state.items.filter((i) => i.sending && !(i.kind === "message" && stored(i.text.trim())));
          this.#set({ title: page.head.title, turn: page.head.turn, items: [...page.items, ...sending], hasEarlier: page.hasEarlier });
        })
        .catch((err: unknown) => {
          if (!(err instanceof ProductOwnerError)) throw err;
        });
    }
  }

  #onEvent(event: ProductOwnerEvent): void {
    if (event.type === "thread") {
      if (event.thread.threadId === this.#state.threadId) this.#set({ turn: event.thread.turn, title: event.thread.title });
      return;
    }
    if (event.type === "settings") return;
    if (event.type === "delta") {
      if (event.threadId !== this.#state.threadId) return;
      const at = this.#state.items.findIndex((i) => i.itemId === event.itemId);
      const found = this.#state.items[at];
      if (found === undefined) {
        const started: ConversationItem = { kind: "message", itemId: event.itemId, at: new Date(), author: "product_owner", text: event.text, streaming: true, stopped: false };
        this.#set({ items: [...this.#state.items, started] });
      } else if (found.kind === "message") {
        this.#set({ items: this.#state.items.map((i, n) => (n === at ? { ...found, text: found.text + event.text, streaming: true } : i)) });
      }
      return;
    }
    this.#onItem(event.threadId, event.item);
  }

  #onItem(threadId: string, item: ThreadItem): void {
    const flight = this.#inFlight;
    const isEcho = flight !== null && item.kind === "message" && item.author === "person" && item.text.trim() === flight.text && (this.#state.threadId === null || this.#state.threadId === threadId);
    if (isEcho) {
      // The server's copy of the message in flight: it stands in for the local one.
      this.#own.add(item.itemId);
      this.#set({ threadId, items: this.#state.items.map((i) => (i.itemId === flight.localId ? { ...item } : i)), fresh: false });
      return;
    }
    if (threadId !== this.#state.threadId) return;
    const fromOtherTab = item.kind === "message" && item.author === "person" && !this.#own.has(item.itemId) ? true : undefined;
    const next: ConversationItem = fromOtherTab ? { ...item, fromOtherTab } : item;
    const at = this.#state.items.findIndex((i) => i.itemId === item.itemId);
    if (at >= 0) {
      // The final item is authoritative over the deltas and over an earlier copy.
      this.#set({ items: this.#state.items.map((i, n) => (n === at ? { ...next, ...(i.fromOtherTab ? { fromOtherTab: true } : {}) } : i)) });
    } else {
      this.#set({ items: [...this.#state.items, next], fresh: false });
    }
  }

  #detach(): void {
    this.#unfollow?.();
    this.#unfollow = null;
    this.#generation++;
    this.#inFlight = null;
  }

  #set(patch: Partial<ConversationState>): void {
    this.#state = { ...this.#state, ...patch };
    this.#emit();
  }

  #emit(): void {
    for (const listener of [...this.#listeners]) listener(this.#state);
  }
}
