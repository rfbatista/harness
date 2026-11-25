// A project's memory, kept live: the Memory tab, the count in a fresh thread's intro and the
// notes in a thread all read it. It reads when it is attached, again on each memory event
// and after the stream resynced, and tells which entries arrived after the first read.

import type { FeedStatus } from "../domain/feed-status";
import type { MemoryEntry } from "../domain/thread";
import { ProductOwnerError, type ProductOwnerEvents, type ProductOwnerGateway } from "./ports";

/** An entry the person deleted here: a one-line receipt stands where it was. */
export interface Receipt {
  readonly entryId: string;
  readonly text: string;
  readonly at: Date;
  /** Where in the list it stood. */
  readonly index: number;
}

export interface ProjectMemoryState {
  readonly projectId: string;
  /** The newest first. */
  readonly entries: readonly MemoryEntry[];
  /** The first read answered. */
  readonly loaded: boolean;
  readonly loading: boolean;
  readonly error: string;
  /** Entries that arrived, or changed, after the first read. */
  readonly fresh: ReadonlySet<string>;
  readonly receipts: readonly Receipt[];
}

const EMPTY: ProjectMemoryState = { projectId: "", entries: [], loaded: false, loading: false, error: "", fresh: new Set(), receipts: [] };

export interface ProjectMemoryDeps {
  readonly gateway: ProductOwnerGateway;
  readonly events: ProductOwnerEvents;
  readonly clock?: () => Date;
}

export class ProjectMemory {
  readonly #deps: ProjectMemoryDeps;
  readonly #listeners = new Set<(state: ProjectMemoryState) => void>();
  #state: ProjectMemoryState = EMPTY;
  #unfollow: (() => void) | null = null;
  #generation = 0;

  constructor(deps: ProjectMemoryDeps) {
    this.#deps = deps;
  }

  get state(): ProjectMemoryState {
    return this.#state;
  }

  subscribe = (listener: (state: ProjectMemoryState) => void): (() => void) => {
    this.#listeners.add(listener);
    listener(this.#state);
    return () => void this.#listeners.delete(listener);
  };

  /** Reads and follows `projectId`'s memory. The returned function stops. */
  attach(projectId: string): () => void {
    this.#stop();
    const generation = ++this.#generation;
    this.#state = { ...EMPTY, projectId, loading: true };
    this.#emit();
    this.#unfollow = this.#deps.events.follow(
      projectId,
      (event) => {
        if (event.type === "memory") void this.#read(generation);
      },
      (status: FeedStatus) => {
        if (status === "resynced") void this.#read(generation);
      },
    );
    void this.#read(generation);
    return () => {
      if (generation === this.#generation) this.#stop();
    };
  }

  /** Reads again after a failed read. */
  retry(): void {
    if (this.#state.projectId === "") return;
    this.#set({ loading: true, error: "" });
    void this.#read(this.#generation);
  }

  /** The person's edit. Answers the entry; rejects with the server's refusal. */
  async update(entryId: string, text: string): Promise<MemoryEntry> {
    const generation = this.#generation;
    try {
      const entry = await this.#deps.gateway.updateMemoryEntry(entryId, text);
      if (generation === this.#generation) this.#set({ entries: this.#state.entries.map((e) => (e.entryId === entryId ? entry : e)) });
      return entry;
    } catch (err) {
      if (err instanceof ProductOwnerError && err.code === "MEMORY_ENTRY_NOT_FOUND" && generation === this.#generation) this.#leave(entryId);
      throw err;
    }
  }

  /** The person's delete: the entry leaves the list and a receipt stands in its place. */
  async remove(entryId: string): Promise<void> {
    const generation = this.#generation;
    // The event the delete causes may read the list again before this call returns: take the entry first.
    const index = this.#state.entries.findIndex((e) => e.entryId === entryId);
    const gone = this.#state.entries[index];
    try {
      await this.#deps.gateway.deleteMemoryEntry(entryId);
    } catch (err) {
      // Gone already (the product owner dropped it, or another tab did): the person's aim is met.
      if (!(err instanceof ProductOwnerError && err.code === "MEMORY_ENTRY_NOT_FOUND")) throw err;
    }
    if (generation !== this.#generation) return;
    const clock = this.#deps.clock ?? (() => new Date());
    this.#set({
      entries: this.#state.entries.filter((e) => e.entryId !== entryId),
      receipts: gone === undefined ? this.#state.receipts : [...this.#state.receipts, { entryId, text: gone.text, at: clock(), index }],
    });
  }

  #leave(entryId: string): void {
    this.#set({ entries: this.#state.entries.filter((e) => e.entryId !== entryId) });
  }

  async #read(generation: number): Promise<void> {
    const projectId = this.#state.projectId;
    try {
      const read = [...(await this.#deps.gateway.memory(projectId))];
      if (generation !== this.#generation) return;
      const before = this.#state;
      const known = new Map(before.entries.map((e) => [e.entryId, e]));
      const fresh = new Set(before.fresh);
      if (before.loaded) {
        for (const e of read) {
          const old = known.get(e.entryId);
          if (old === undefined || old.changedAt?.getTime() !== e.changedAt?.getTime() || old.text !== e.text) fresh.add(e.entryId);
        }
      }
      // A receipt stays only until the list no longer holds the entry (it never does again).
      this.#set({ entries: read, loaded: true, loading: false, error: "", fresh });
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

  #set(patch: Partial<ProjectMemoryState>): void {
    this.#state = { ...this.#state, ...patch };
    this.#emit();
  }

  #emit(): void {
    for (const listener of [...this.#listeners]) listener(this.#state);
  }
}
