// A project's digests, newest first, with the settings the Digests tab needs to explain them: the
// switch, the digest time and the server's time zone.

import type { Digest } from "../domain/digest";
import { ProductOwnerError, type ProductOwnerEvents, type ProductOwnerGateway } from "./ports";

export interface ProjectDigestsState {
  /** The first page and the settings are read. */
  readonly loaded: boolean;
  /** Why the first read failed; "" otherwise. */
  readonly error: string;
  /** null until the settings are read. */
  readonly enabled: boolean | null;
  readonly digestTime: string | null;
  readonly timeZone: string;
  /** When the next digest is recorded, as the server says; null for none. */
  readonly nextDigestAt: Date | null;
  /** The newest first, one per date. */
  readonly digests: readonly Digest[];
  readonly hasEarlier: boolean;
  /** An earlier page is on its way. */
  readonly loadingEarlier: boolean;
  /** Why the last earlier page failed; "" otherwise. */
  readonly earlierError: string;
  /** Turning the product owner on is on its way. */
  readonly turningOn: boolean;
  /** Why turning it on failed; "" otherwise. */
  readonly turnOnError: string;
}

/** `fresh` replaces the digests of its dates among `held`; one per date, the newest first. */
function merge(held: readonly Digest[], fresh: readonly Digest[]): Digest[] {
  const dates = new Set(fresh.map((d) => d.date));
  return [...fresh, ...held.filter((d) => !dates.has(d.date))].sort((a, b) => b.date.localeCompare(a.date));
}

const INITIAL: ProjectDigestsState = { loaded: false, error: "", enabled: null, digestTime: null, timeZone: "", nextDigestAt: null, digests: [], hasEarlier: false, loadingEarlier: false, earlierError: "", turningOn: false, turnOnError: "" };

export class ProjectDigests {
  readonly #deps: { gateway: ProductOwnerGateway; events: ProductOwnerEvents };
  readonly #listeners = new Set<(state: ProjectDigestsState) => void>();
  #state: ProjectDigestsState = INITIAL;
  #projectId = "";
  #unfollow: (() => void) | null = null;
  #generation = 0;

  constructor(deps: { gateway: ProductOwnerGateway; events: ProductOwnerEvents }) {
    this.#deps = deps;
  }

  get state(): ProjectDigestsState {
    return this.#state;
  }

  subscribe = (listener: (state: ProjectDigestsState) => void): (() => void) => {
    this.#listeners.add(listener);
    listener(this.#state);
    return () => void this.#listeners.delete(listener);
  };

  /** Reads and follows `projectId`'s digests. The returned function stops. */
  attach(projectId: string): () => void {
    this.#stop();
    const generation = ++this.#generation;
    this.#projectId = projectId;
    this.#set(INITIAL);
    this.#unfollow = this.#deps.events.follow(
      projectId,
      (event) => {
        if (event.type === "settings") this.#set({ enabled: event.settings.enabled, digestTime: event.settings.digestTime, timeZone: event.settings.timeZone, nextDigestAt: event.settings.nextDigestAt });
        else if (event.type === "digest") void this.#refresh(generation);
      },
      (status) => {
        // After the stream was down, what arrived meanwhile is read again.
        if (status === "resynced") void (this.#state.loaded ? this.#refresh(generation) : this.#read(generation));
      },
    );
    void this.#read(generation);
    return () => {
      if (generation === this.#generation) this.#stop();
    };
  }

  /** Reads again what the first read could not. */
  retry(): void {
    const generation = ++this.#generation;
    this.#set({ error: "" });
    void this.#read(generation);
  }

  /** The page of digests before the oldest held, added below them. One page at a time. */
  async loadEarlier(): Promise<void> {
    const { digests, hasEarlier, loadingEarlier } = this.#state;
    const oldest = digests.at(-1);
    if (!hasEarlier || loadingEarlier || !oldest) return;
    const generation = this.#generation;
    this.#set({ loadingEarlier: true, earlierError: "" });
    try {
      const page = await this.#deps.gateway.digests(this.#projectId, oldest.date);
      if (generation === this.#generation) this.#set({ loadingEarlier: false, digests: merge(this.#state.digests, page.digests), hasEarlier: page.hasEarlier });
    } catch (err) {
      if (!(err instanceof ProductOwnerError)) throw err;
      if (generation === this.#generation) this.#set({ loadingEarlier: false, earlierError: err.message });
    }
  }

  /** Loads earlier pages until `date` is among the digests or there are no more. Whether it is. */
  async reveal(date: string): Promise<boolean> {
    await this.#read1st();
    for (;;) {
      const { digests, hasEarlier, loadingEarlier, error } = this.#state;
      if (digests.some((d) => d.date === date)) return true;
      const oldest = digests.at(-1);
      if (error !== "" || !hasEarlier || !oldest || oldest.date < date) return false;
      if (loadingEarlier) {
        await new Promise((resolve) => setTimeout(resolve, 0));
        continue;
      }
      const before = this.#state.digests.length;
      await this.loadEarlier();
      if (this.#state.digests.length === before) return false;
    }
  }

  /** Resolves once the first read has an answer, or failed. */
  #read1st(): Promise<void> {
    return new Promise((resolve) => {
      const stop = this.subscribe((state) => {
        if (state.loaded || state.error !== "") queueMicrotask(() => (stop(), resolve()));
      });
    });
  }

  /** "Turn it on", from the off banner. A refusal says why. */
  async turnOn(): Promise<void> {
    if (this.#state.turningOn) return;
    const generation = this.#generation;
    this.#set({ turningOn: true, turnOnError: "" });
    try {
      const settings = await this.#deps.gateway.setEnabled(this.#projectId, true);
      if (generation === this.#generation) this.#set({ turningOn: false, enabled: settings.enabled });
    } catch (err) {
      if (!(err instanceof ProductOwnerError)) throw err;
      if (generation === this.#generation) this.#set({ turningOn: false, turnOnError: err.message });
    }
  }

  async #read(generation: number): Promise<void> {
    try {
      const [settings, page] = await Promise.all([this.#deps.gateway.settings(this.#projectId), this.#deps.gateway.digests(this.#projectId)]);
      if (generation !== this.#generation) return;
      this.#set({ loaded: true, error: "", enabled: settings.enabled, digestTime: settings.digestTime, timeZone: settings.timeZone, nextDigestAt: settings.nextDigestAt, digests: page.digests, hasEarlier: page.hasEarlier });
    } catch (err) {
      if (!(err instanceof ProductOwnerError)) throw err;
      if (generation === this.#generation) this.#set({ error: err.message });
    }
  }

  /** Reads the newest page again, for a digest that was recorded: it joins or replaces the digests held, and nothing held moves. */
  async #refresh(generation: number): Promise<void> {
    try {
      // The settings too: the next run moves when a digest is recorded, and no settings event says so.
      const [settings, page] = await Promise.all([this.#deps.gateway.settings(this.#projectId), this.#deps.gateway.digests(this.#projectId)]);
      // Earlier digests exist beyond the oldest held exactly when they did before: a new digest is only ever newer.
      if (generation === this.#generation) this.#set({ enabled: settings.enabled, digestTime: settings.digestTime, timeZone: settings.timeZone, nextDigestAt: settings.nextDigestAt, digests: merge(this.#state.digests, page.digests), hasEarlier: this.#state.loaded ? this.#state.hasEarlier : page.hasEarlier });
    } catch (err) {
      if (!(err instanceof ProductOwnerError)) throw err;
    }
  }

  #stop(): void {
    this.#unfollow?.();
    this.#unfollow = null;
    this.#generation++;
  }

  #set(patch: Partial<ProjectDigestsState>): void {
    this.#state = { ...this.#state, ...patch };
    for (const listener of [...this.#listeners]) listener(this.#state);
  }
}
