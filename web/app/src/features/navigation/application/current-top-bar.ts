// The top bar on screen. The picker and the links both show it, and it changes
// as the person moves between pages.

import type { TopBar } from "../domain/top-bar";

export class CurrentTopBar {
  #topBar: TopBar;
  readonly #listeners = new Set<(topBar: TopBar) => void>();

  constructor(initial: TopBar) {
    this.#topBar = initial;
  }

  get(): TopBar {
    return this.#topBar;
  }

  set(next: TopBar): void {
    this.#topBar = next;
    for (const listener of [...this.#listeners]) listener(next);
  }

  /** Calls `listener` with the top bar now and with every change; returns its unsubscribe. */
  subscribe(listener: (topBar: TopBar) => void): () => void {
    this.#listeners.add(listener);
    listener(this.#topBar);
    return () => {
      this.#listeners.delete(listener);
    };
  }
}
