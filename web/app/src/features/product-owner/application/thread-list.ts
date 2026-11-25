// A project's threads, the most recently active first, kept live: a thread that starts, is
// renamed or changes its turn shows at once. The thread list popup and the page's side
// column both read it.

import type { FeedStatus } from "../domain/feed-status";
import type { ThreadSummary } from "../domain/thread";
import { ProductOwnerError, type ProductOwnerEvents, type ProductOwnerGateway } from "./ports";

export interface ThreadListState {
  readonly threads: readonly ThreadSummary[];
  readonly loading: boolean;
  readonly error: string;
}

const newestFirst = (a: ThreadSummary, b: ThreadSummary): number => b.lastActivityAt.getTime() - a.lastActivityAt.getTime();

export class ThreadList {
  readonly #deps: { gateway: ProductOwnerGateway; events: ProductOwnerEvents };
  readonly #listeners = new Set<(state: ThreadListState) => void>();
  #state: ThreadListState = { threads: [], loading: false, error: "" };
  #unfollow: (() => void) | null = null;
  #generation = 0;

  constructor(deps: { gateway: ProductOwnerGateway; events: ProductOwnerEvents }) {
    this.#deps = deps;
  }

  get state(): ThreadListState {
    return this.#state;
  }

  subscribe = (listener: (state: ThreadListState) => void): (() => void) => {
    this.#listeners.add(listener);
    listener(this.#state);
    return () => void this.#listeners.delete(listener);
  };

  /** Reads and follows `projectId`'s threads. The returned function stops. */
  attach(projectId: string): () => void {
    this.#stop();
    const generation = ++this.#generation;
    this.#set({ threads: [], loading: true, error: "" });
    this.#unfollow = this.#deps.events.follow(
      projectId,
      (event) => {
        if (event.type !== "thread") return;
        const rest = this.#state.threads.filter((t) => t.threadId !== event.thread.threadId);
        this.#set({ threads: [...rest, event.thread].sort(newestFirst) });
      },
      (status: FeedStatus) => {
        if (status === "resynced") void this.#read(projectId, generation);
      },
    );
    void this.#read(projectId, generation);
    return () => {
      if (generation === this.#generation) this.#stop();
    };
  }

  async #read(projectId: string, generation: number): Promise<void> {
    try {
      const threads = await this.#deps.gateway.threads(projectId);
      if (generation === this.#generation) this.#set({ threads: [...threads].sort(newestFirst), loading: false, error: "" });
    } catch (err) {
      if (!(err instanceof ProductOwnerError)) throw err;
      if (generation === this.#generation) this.#set({ loading: false, error: err.message });
    }
  }

  #stop(): void {
    this.#unfollow?.();
    this.#unfollow = null;
    this.#generation++;
  }

  #set(patch: Partial<ThreadListState>): void {
    this.#state = { ...this.#state, ...patch };
    for (const listener of [...this.#listeners]) listener(this.#state);
  }
}
