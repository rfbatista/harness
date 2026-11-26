// A service's log inside the panel: read once, then followed live from where
// the read ended. Scrolling up pauses following.

import { Presenter } from "../../../../shared/kernel/presenter";
import type { ServicesSource } from "../../application/ports";
import { EMPTY_LOG, applyChunk, fromRead, type LogLine, type LogState } from "../../domain/log";

export interface LogViewModel {
  readonly loading: boolean;
  readonly error: string;
  readonly lines: readonly LogLine[];
  readonly following: boolean;
  readonly live: boolean;
  readonly empty: boolean;
}

export class LogPresenter extends Presenter<LogViewModel> {
  readonly #source: ServicesSource;
  readonly #serviceId: string;
  #log: LogState = EMPTY_LOG;
  #stop: () => void = () => {};
  #stopped = false;

  constructor(source: ServicesSource, serviceId: string) {
    super({ loading: true, error: "", lines: [], following: true, live: false, empty: false });
    this.#source = source;
    this.#serviceId = serviceId;
  }

  get serviceId(): string {
    return this.#serviceId;
  }

  async start(): Promise<void> {
    await this.#load();
  }

  stop(): void {
    this.#stopped = true;
    this.#stop();
  }

  /** The person scrolled: up pauses following; back at the bottom resumes it. */
  scrolled(atBottom: boolean): void {
    if (this.viewModel.following !== atBottom) this.update((vm) => ({ ...vm, following: atBottom }));
  }

  resume(): void {
    this.scrolled(true);
  }

  async #load(): Promise<void> {
    this.#stop();
    this.update((vm) => ({ ...vm, loading: true, error: "" }));
    try {
      this.#log = fromRead(await this.#source.readLog(this.#serviceId));
    } catch (err) {
      if (this.#stopped) return;
      this.update((vm) => ({ ...vm, loading: false, error: err instanceof Error ? err.message : "The log did not load." }));
      return;
    }
    if (this.#stopped) return;
    this.#show(false);
    this.#stop = this.#source.followLog(
      this.#serviceId,
      this.#log.end,
      (event) => {
        if (event.kind === "reset") {
          void this.#load();
          return;
        }
        const next = applyChunk(this.#log, event);
        if (next.gap) {
          void this.#load();
          return;
        }
        this.#log = next.state;
        this.#show(this.viewModel.live);
      },
      (status: string) => {
        this.update((vm) => ({ ...vm, live: status === "live" }));
        if (status === "resynced") void this.#load();
      },
    );
  }

  #show(live: boolean): void {
    const lines = this.#log.partial === "" ? this.#log.lines : [...this.#log.lines, { text: this.#log.partial, tone: "plain" as const }];
    this.update((vm) => ({ ...vm, loading: false, error: "", lines, live, empty: lines.length === 0 }));
  }
}
