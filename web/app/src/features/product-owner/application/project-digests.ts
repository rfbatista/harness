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
  /** The newest first, one per date. */
  readonly digests: readonly Digest[];
  readonly hasEarlier: boolean;
  /** Turning the product owner on is on its way. */
  readonly turningOn: boolean;
  /** Why turning it on failed; "" otherwise. */
  readonly turnOnError: string;
}

const INITIAL: ProjectDigestsState = { loaded: false, error: "", enabled: null, digestTime: null, timeZone: "", digests: [], hasEarlier: false, turningOn: false, turnOnError: "" };

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
        if (event.type === "settings") this.#set({ enabled: event.settings.enabled, digestTime: event.settings.digestTime, timeZone: event.settings.timeZone });
      },
      () => {},
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
      this.#set({ loaded: true, error: "", enabled: settings.enabled, digestTime: settings.digestTime, timeZone: settings.timeZone, digests: page.digests, hasEarlier: page.hasEarlier });
    } catch (err) {
      if (!(err instanceof ProductOwnerError)) throw err;
      if (generation === this.#generation) this.#set({ error: err.message });
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
