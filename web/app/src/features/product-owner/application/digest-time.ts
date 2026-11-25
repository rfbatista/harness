// A project's digest time on the Settings page: read, followed on the settings event, set and cleared.

import type { FeedStatus } from "../domain/feed-status";
import { ProductOwnerError, type ProductOwnerEvents, type ProductOwnerGateway } from "./ports";

export interface DigestTimeState {
  /** The settings are read. */
  readonly loaded: boolean;
  /** Why the first read failed; "" otherwise. */
  readonly error: string;
  /** null until read, and while the product owner is on or off alike. */
  readonly enabled: boolean | null;
  /** "HH:MM" in the server's time zone; null while none is set. */
  readonly digestTime: string | null;
  readonly timeZone: string;
  /** A change is on its way. */
  readonly busy: boolean;
  /** Why the last change was refused; "" otherwise. */
  readonly saveError: string;
  /** Counts the refused changes, so a view can put its input back to the time that holds. */
  readonly refusals: number;
}

const INITIAL: DigestTimeState = { loaded: false, error: "", enabled: null, digestTime: null, timeZone: "", busy: false, saveError: "", refusals: 0 };

export class DigestTime {
  readonly #deps: { gateway: ProductOwnerGateway; events: ProductOwnerEvents };
  readonly #listeners = new Set<(state: DigestTimeState) => void>();
  #state: DigestTimeState = INITIAL;
  #projectId = "";
  #unfollow: (() => void) | null = null;
  #generation = 0;
  /** What was asked for while a change was on its way: the last one is made next. */
  #pending: string | null = null;

  constructor(deps: { gateway: ProductOwnerGateway; events: ProductOwnerEvents }) {
    this.#deps = deps;
  }

  get state(): DigestTimeState {
    return this.#state;
  }

  subscribe = (listener: (state: DigestTimeState) => void): (() => void) => {
    this.#listeners.add(listener);
    listener(this.#state);
    return () => void this.#listeners.delete(listener);
  };

  /** Reads and follows `projectId`'s digest time. The returned function stops. */
  attach(projectId: string): () => void {
    this.#stop();
    const generation = ++this.#generation;
    this.#projectId = projectId;
    this.#set(INITIAL);
    this.#unfollow = this.#deps.events.follow(
      projectId,
      (event) => {
        if (event.type === "settings") this.#set({ loaded: true, enabled: event.settings.enabled, digestTime: event.settings.digestTime, timeZone: event.settings.timeZone });
        // The next run moves when a digest is recorded, and no settings event says so.
        else if (event.type === "digest") void this.#read(generation);
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

  retry(): void {
    void this.#read(this.#generation);
  }

  /** Sets the time ("HH:MM"), or clears it with "". A refusal leaves the time as it was and says why. One change at a time: a time asked for meanwhile follows it. */
  async set(time: string): Promise<void> {
    const { busy, loaded } = this.#state;
    if (!loaded) return;
    if (busy) {
      this.#pending = time;
      return;
    }
    const generation = this.#generation;
    this.#set({ busy: true, saveError: "" });
    try {
      const settings = await this.#deps.gateway.updateSettings(this.#projectId, { digestTime: time });
      if (generation === this.#generation) this.#set({ busy: false, digestTime: settings.digestTime, timeZone: settings.timeZone, enabled: settings.enabled });
    } catch (err) {
      if (!(err instanceof ProductOwnerError)) throw err;
      if (generation === this.#generation) this.#set({ busy: false, saveError: err.message, refusals: this.#state.refusals + 1 });
    }
    const next = this.#pending;
    this.#pending = null;
    if (next !== null && generation === this.#generation && next !== this.#state.digestTime) await this.set(next);
  }

  async #read(generation: number): Promise<void> {
    try {
      const settings = await this.#deps.gateway.settings(this.#projectId);
      if (generation === this.#generation) this.#set({ loaded: true, error: "", enabled: settings.enabled, digestTime: settings.digestTime, timeZone: settings.timeZone });
    } catch (err) {
      if (!(err instanceof ProductOwnerError)) throw err;
      if (generation === this.#generation && !this.#state.loaded) this.#set({ error: err.message });
    }
  }

  #stop(): void {
    this.#unfollow?.();
    this.#unfollow = null;
    this.#generation++;
  }

  #set(patch: Partial<DigestTimeState>): void {
    this.#state = { ...this.#state, ...patch };
    for (const listener of [...this.#listeners]) listener(this.#state);
  }
}
