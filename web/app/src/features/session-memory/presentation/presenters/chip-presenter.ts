// The memory of a session in its head: the chip, and the popover behind it. The last sample of the session
// against its orçamento comes from the initial read first and then from the session's stream. The chip shows
// nothing until there is a value to show.

import type { Clock } from "../../../../shared/kernel/clock";
import { formatBudget, formatShown } from "../../../../shared/kernel/memory/bytes";
import { initialTracker, settle, type Level, type Tracker } from "../../../../shared/kernel/memory/level";
import type { MemoryPrevention, MemorySource, MemoryStream } from "../../../../shared/kernel/memory/ports";
import { hasValue, killKey, shareOf, type BudgetSource, type MemoryKill, type MemorySample } from "../../../../shared/kernel/memory/sample";
import { memoryView, sameView, type MemoryView } from "../../../../shared/kernel/memory/view";
import { Presenter } from "../../../../shared/kernel/presenter";

export interface KillRow {
  key: string;
  time: string;
  command: string;
  rss: string;
  signal: string;
}

export interface PopoverView {
  title: string;
  state: MemoryView["state"];
  fillPercent: number;
  processes: string;
  heaviest: string;
  /** The orçamento, when it was fixed, and (once the read is in) where it came from. */
  budget: string;
  /** "" until the read is in. */
  prevention: string;
  /** Newest first. */
  kills: KillRow[];
  /** What stands in for an empty list: "Nothing yet.", or why nothing was ended; "" when there are kills. */
  killsNote: string;
}

export interface ChipViewModel {
  /** null: no chip (not live, not read yet, no sample, or the session ended). */
  readonly view: MemoryView | null;
  /** null: the popover is closed (or there is no chip). */
  readonly popover: PopoverView | null;
}

export interface ChipDeps {
  source: MemorySource;
  stream: MemoryStream;
  clock: Clock;
  sessionId: string;
  live: boolean;
  /** A kill's time as the person reads it. */
  time?: (at: Date) => string;
}

const SOURCES: Record<BudgetSource, string> = { task: "from this task", repository: "from this repository", global: "from the global setting" };
const WORDS: Record<Level, string> = { normal: "", near: " · near budget", over: " · over budget" };
const AGENT_OVER = "Nothing. The agent itself is over the orçamento, and the vigia never ends the agent.";

const samePopover = (a: PopoverView | null, b: PopoverView | null): boolean => {
  if (a === null || b === null) return a === b;
  const flat = (p: PopoverView): string => [p.title, p.state, p.fillPercent, p.processes, p.heaviest, p.budget, p.prevention, p.killsNote, ...p.kills.flatMap((k) => [k.key, k.time, k.command, k.rss, k.signal])].join("\u0001");
  return flat(a) === flat(b);
};

const plural = (n: number, word: string): string => `${n} ${word}${n === 1 ? "" : "s"}`;

export class ChipPresenter extends Presenter<ChipViewModel> {
  readonly #deps: ChipDeps;
  #tracker: Tracker = initialTracker();
  #sample: MemorySample | null = null;
  #kills = new Map<string, MemoryKill>();
  #source: BudgetSource | null = null;
  #prevention: MemoryPrevention | null = null;
  #open = false;
  #reading: Promise<void> = Promise.resolve();
  #stop: (() => void) | null = null;

  constructor(deps: ChipDeps) {
    super({ view: null, popover: null });
    this.#deps = deps;
  }

  /** Follows the stream and does the initial read. Resolves when the read is in. */
  async start(): Promise<void> {
    if (!this.#deps.live || this.#stop) return;
    this.#stop = this.#deps.stream.follow(this.#deps.sessionId, {
      onSample: (sample) => this.#take(sample),
      onKill: (kill) => this.#addKill(kill),
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

  /** Opens the popover on a fresh read. Nothing opens while there is no chip. */
  async open(): Promise<void> {
    if (this.viewModel.view === null) return;
    this.#open = true;
    this.#render();
    this.#reading = this.#read();
    await this.#reading;
  }

  close(): void {
    this.#open = false;
    this.#render();
  }

  async toggle(): Promise<void> {
    if (this.#open) this.close();
    else await this.open();
  }

  /** Stops following the stream. */
  dispose(): void {
    this.#stop?.();
    this.#stop = null;
  }

  async #read(): Promise<void> {
    try {
      const read = await this.#deps.source.read(this.#deps.sessionId);
      if (read === null) return;
      this.#source = read.budgetSource;
      this.#prevention = read.prevention;
      for (const kill of read.kills) this.#kills.set(killKey(kill), kill);
      const have = this.#sample?.sampledAt;
      const got = read.sample.sampledAt;
      // A sample that came by the stream while the read was out is newer than what the read saw.
      if (have && (got === null || got < have)) this.#render();
      else this.#take(read.sample);
    } catch {
      // The last value stays; the next sample or reconnect brings the chip up to date.
    }
  }

  #take(sample: MemorySample): void {
    if (sample.sessionId !== this.#deps.sessionId) return;
    this.#sample = sample;
    this.#tracker = hasValue(sample) ? settle(this.#tracker, shareOf(sample), this.#deps.clock.now()) : initialTracker();
    this.#render();
  }

  #addKill(kill: MemoryKill): void {
    if (kill.sessionId !== this.#deps.sessionId) return;
    this.#kills.set(killKey(kill), kill);
    this.#render();
  }

  #render(): void {
    const view = memoryView(this.#sample, this.#tracker.level, null);
    if (view === null) this.#open = false;
    const popover = view && this.#open ? this.#popover(view) : null;
    const now = this.viewModel;
    if (sameView(view, now.view) && samePopover(popover, now.popover)) return;
    this.set({ view: sameView(view, now.view) ? now.view : view, popover });
  }

  #popover(view: MemoryView): PopoverView {
    const sample = this.#sample!;
    const time = this.#deps.time ?? ((at: Date) => at.toISOString().slice(11, 16));
    const kills = [...this.#kills.values()].sort((a, b) => b.killedAt.getTime() - a.killedAt.getTime());
    const budget = formatBudget(sample.budgetBytes);
    const p = this.#prevention;
    return {
      title: `Memory · ${formatShown(sample.rssBytes)} of ${budget}${WORDS[this.#tracker.level]}`,
      state: view.state,
      fillPercent: view.fillPercent,
      processes: String(sample.processes),
      heaviest: sample.heaviest ? `${sample.heaviest.command} · ${formatShown(sample.heaviest.rssBytes)}` : "none eligible",
      budget: `${budget}, fixed when the session started${this.#source ? ` · ${SOURCES[this.#source]}` : ""}`,
      prevention: p ? `${plural(p.testWorkers, "test worker")} · ${p.heapMb} MB heap` : "",
      kills: kills.map((k) => ({ key: killKey(k), time: time(k.killedAt), command: k.command, rss: formatShown(k.rssBytes), signal: k.signal })),
      killsNote: kills.length > 0 ? "" : this.#tracker.level === "over" ? AGENT_OVER : "Nothing yet.",
    };
  }
}
