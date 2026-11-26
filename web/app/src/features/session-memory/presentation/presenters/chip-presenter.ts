// The memory chip of the session head: the last sample of the session against its orçamento, from the initial
// read first and then from the session's stream. It shows nothing until there is a value to show.

import type { Clock } from "../../../../shared/kernel/clock";
import { initialTracker, settle, type Tracker } from "../../../../shared/kernel/memory/level";
import type { MemorySource, MemoryStream } from "../../../../shared/kernel/memory/ports";
import { hasValue, shareOf, type MemorySample } from "../../../../shared/kernel/memory/sample";
import { memoryView, sameView, type MemoryView } from "../../../../shared/kernel/memory/view";
import { Presenter } from "../../../../shared/kernel/presenter";

export interface ChipViewModel {
  /** null: no chip (not live, not read yet, no sample, or the session ended). */
  readonly view: MemoryView | null;
}

export interface ChipDeps {
  source: MemorySource;
  stream: MemoryStream;
  clock: Clock;
  sessionId: string;
  live: boolean;
}

export class ChipPresenter extends Presenter<ChipViewModel> {
  readonly #deps: ChipDeps;
  #tracker: Tracker = initialTracker();
  #sample: MemorySample | null = null;
  #reading: Promise<void> = Promise.resolve();
  #stop: (() => void) | null = null;

  constructor(deps: ChipDeps) {
    super({ view: null });
    this.#deps = deps;
  }

  /** Follows the stream and does the initial read. Resolves when the read is in. */
  async start(): Promise<void> {
    if (!this.#deps.live || this.#stop) return;
    this.#stop = this.#deps.stream.follow(this.#deps.sessionId, {
      onSample: (sample) => this.#take(sample),
      onKill: () => {},
      onResync: () => {
        this.#reading = this.#read();
      },
    });
    this.#reading = this.#read();
    await this.#reading;
  }

  /** Resolves when the read in flight is in (a reconnect starts one). */
  settled(): Promise<void> {
    return this.#reading;
  }

  close(): void {
    this.#stop?.();
    this.#stop = null;
  }

  async #read(): Promise<void> {
    try {
      const read = await this.#deps.source.read(this.#deps.sessionId);
      if (read === null) return;
      const have = this.#sample?.sampledAt;
      const got = read.sample.sampledAt;
      // A sample that came by the stream while the read was out is newer than what the read saw.
      if (have && (got === null || got < have)) return;
      this.#take(read.sample);
    } catch {
      // The last value stays; the next sample or reconnect brings the chip up to date.
    }
  }

  #take(sample: MemorySample): void {
    if (sample.sessionId !== this.#deps.sessionId) return;
    this.#sample = sample;
    if (!hasValue(sample)) {
      this.#tracker = initialTracker();
    } else {
      this.#tracker = settle(this.#tracker, shareOf(sample), this.#deps.clock.now());
    }
    const view = memoryView(sample, this.#tracker.level, null);
    if (!sameView(view, this.viewModel.view)) this.set({ view });
  }
}
