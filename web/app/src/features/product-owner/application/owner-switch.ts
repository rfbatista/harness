// The product owner's on/off switch on the project's Settings page. It follows the settings
// event, so the toggle and the panel of any open tab and this switch agree.

import type { FeedStatus } from "../domain/feed-status";
import { ProductOwnerError, type ProductOwnerEvents, type ProductOwnerGateway } from "./ports";

export interface OwnerSwitchState {
  /** null until the switch is read. */
  readonly enabled: boolean | null;
  /** A change is on its way. */
  readonly busy: boolean;
  /** Why the last read or change failed; "" otherwise. */
  readonly error: string;
}

export class OwnerSwitch {
  readonly #deps: { gateway: ProductOwnerGateway; events: ProductOwnerEvents };
  readonly #listeners = new Set<(state: OwnerSwitchState) => void>();
  #state: OwnerSwitchState = { enabled: null, busy: false, error: "" };
  #projectId = "";
  #unfollow: (() => void) | null = null;
  #generation = 0;

  constructor(deps: { gateway: ProductOwnerGateway; events: ProductOwnerEvents }) {
    this.#deps = deps;
  }

  get state(): OwnerSwitchState {
    return this.#state;
  }

  subscribe = (listener: (state: OwnerSwitchState) => void): (() => void) => {
    this.#listeners.add(listener);
    listener(this.#state);
    return () => void this.#listeners.delete(listener);
  };

  /** Reads and follows the project's switch. The returned function stops. */
  attach(projectId: string): () => void {
    this.#stop();
    const generation = ++this.#generation;
    this.#projectId = projectId;
    this.#set({ enabled: null, busy: false, error: "" });
    this.#unfollow = this.#deps.events.follow(
      projectId,
      (event) => {
        if (event.type === "settings" && event.settings.projectId === projectId) this.#set({ enabled: event.settings.enabled });
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

  /** Flips the switch. A refusal leaves it where it was and says why. */
  async toggle(): Promise<void> {
    const { enabled, busy } = this.#state;
    if (enabled === null || busy) return;
    const generation = this.#generation;
    this.#set({ busy: true, error: "" });
    try {
      const settings = await this.#deps.gateway.setEnabled(this.#projectId, !enabled);
      if (generation === this.#generation) this.#set({ enabled: settings.enabled, busy: false });
    } catch (err) {
      if (!(err instanceof ProductOwnerError)) throw err;
      if (generation === this.#generation) this.#set({ busy: false, error: err.message });
    }
  }

  async #read(generation: number): Promise<void> {
    try {
      const settings = await this.#deps.gateway.settings(this.#projectId);
      if (generation === this.#generation) this.#set({ enabled: settings.enabled, error: "" });
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

  #set(patch: Partial<OwnerSwitchState>): void {
    this.#state = { ...this.#state, ...patch };
    for (const listener of [...this.#listeners]) listener(this.#state);
  }
}
