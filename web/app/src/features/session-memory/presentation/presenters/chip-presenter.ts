// The memory of a session in its head: the chip, and the popover behind it. The last sample of the session
// against its orçamento comes from the initial read first and then from the session's stream. The chip shows
// nothing until there is a value to show.

import type { Clock } from "../../../../shared/kernel/clock";
import { formatBudget, formatShown } from "../../../../shared/kernel/memory/bytes";
import { initialTracker, releaseHold, settle, type Level, type Tracker } from "../../../../shared/kernel/memory/level";
import type { MemoryPrevention, MemorySource, MemoryStream } from "../../../../shared/kernel/memory/ports";
import { CHIP_ENDED_MS, hasValue, killKey, shareOf, type BudgetSource, type MemoryKill, type MemorySample } from "../../../../shared/kernel/memory/sample";
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
  /** The heaviest eligible process, null when there is none. */
  heaviest: { command: string; rss: string } | null;
  /** The orçamento, when it was fixed, and (once the read is in) where it came from. */
  budget: string;
  /** "" until the read is in. */
  prevention: string;
  /** Newest first. */
  kills: KillRow[];
  /** What stands in for an empty list: "Nothing yet.", or why nothing was ended; "" when there are kills. */
  killsNote: string;
}

export interface ToastView {
  key: string;
  title: string;
  rss: string;
  tree: string;
  budget: string;
  /** A SIGKILL: the process did not exit in the 5 s of grace. */
  notExited: boolean;
}

export interface Scheduler {
  /** Runs `fn` after `ms`; returns what cancels it. */
  after(ms: number, fn: () => void): () => void;
}

export interface ChipViewModel {
  /** null: no chip (not live, not read yet, no sample, or the session ended). */
  readonly view: MemoryView | null;
  /** null: the popover is closed (or there is no chip). */
  readonly popover: PopoverView | null;
  /** At most two, the newest first. */
  readonly toasts: readonly ToastView[];
  /** Toasts folded out of sight: "+{n} earlier · in Details". */
  readonly more: number;
  /** The key of the kill that just ended a process, for the chip's single ring; null otherwise. */
  readonly signal: string | null;
}

export interface ChipDeps {
  source: MemorySource;
  stream: MemoryStream;
  clock: Clock;
  sessionId: string;
  live: boolean;
  /** A kill's time as the person reads it. */
  time?: (at: Date) => string;
  scheduler?: Scheduler;
}

const TOAST_MS = 12_000;
const MAX_TOASTS = 2;

interface Toast {
  kill: MemoryKill;
  cancel: (() => void) | null;
  /** When it goes, while its clock runs. */
  until: number;
  /** What holds its clock: "hover", "focus". */
  holds: Set<string>;
  remaining: number;
}

const SOURCES: Record<BudgetSource, string> = { task: "from this task", repository: "from this repository", global: "from the global setting" };
const WORDS: Record<Level, string> = { normal: "", near: " · near budget", over: " · over budget" };
const AGENT_OVER = "Nothing. The agent itself is over the orçamento, and the vigia never ends the agent.";

const samePopover = (a: PopoverView | null, b: PopoverView | null): boolean => {
  if (a === null || b === null) return a === b;
  const flat = (p: PopoverView): string => [p.title, p.state, p.fillPercent, p.processes, p.heaviest?.command, p.heaviest?.rss, p.budget, p.prevention, p.killsNote, ...p.kills.flatMap((k) => [k.key, k.time, k.command, k.rss, k.signal])].join("\u0001");
  return flat(a) === flat(b);
};

const toastView = ({ kill }: Toast): ToastView => ({
  key: killKey(kill),
  title: `${kill.command} ${kill.signal === "SIGKILL" ? "killed" : "ended"}: over the memory budget`,
  rss: formatShown(kill.rssBytes),
  tree: formatShown(kill.treeRssBytes),
  budget: formatBudget(kill.budgetBytes),
  notExited: kill.signal === "SIGKILL",
});

const sameToasts = (a: readonly ToastView[], b: readonly ToastView[]): boolean => a.length === b.length && a.every((t, i) => t.key === b[i]!.key);

const plural = (n: number, word: string): string => `${n} ${word}${n === 1 ? "" : "s"}`;

export class ChipPresenter extends Presenter<ChipViewModel> {
  readonly #deps: ChipDeps;
  #tracker: Tracker = initialTracker();
  #sample: MemorySample | null = null;
  #kills = new Map<string, MemoryKill>();
  #source: BudgetSource | null = null;
  #prevention: MemoryPrevention | null = null;
  #open = false;
  #toasts: Toast[] = [];
  /** The kills that say "ended" on the chip, and until when. */
  #ended: { kill: MemoryKill; until: number }[] = [];
  #signal: string | null = null;
  #loading = 0;
  /** Kills that came while no read had judged them: they wait for a read that works. */
  #buffer: MemoryKill[] = [];
  #reading: Promise<void> = Promise.resolve();
  #stop: (() => void) | null = null;
  #disposed = false;
  /** The reads in flight, to abort on dispose. */
  readonly #reads = new Set<AbortController>();
  #retry: (() => void) | null = null;
  #retries = 0;
  /** The "ended" timers, to cancel on dispose. */
  readonly #timers = new Set<() => void>();

  constructor(deps: ChipDeps) {
    super({ view: null, popover: null, toasts: [], more: 0, signal: null });
    this.#deps = deps;
  }

  /** Follows the stream and does the initial read. Resolves when the read is in. */
  async start(): Promise<void> {
    if (!this.#deps.live || this.#stop) return;
    this.#stop = this.#deps.stream.follow(this.#deps.sessionId, {
      onSample: (sample) => this.#take(sample),
      onKill: (kill) => this.#kill(kill),
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

  /** Stops everything it started: the stream, the timers, the read in flight and what waits for a read. */
  dispose(): void {
    this.#disposed = true;
    this.#stop?.();
    this.#stop = null;
    this.#retry?.();
    this.#retry = null;
    for (const abort of this.#reads) abort.abort();
    for (const cancel of this.#timers) cancel();
    this.#timers.clear();
    for (const toast of this.#toasts) toast.cancel?.();
    this.#buffer = [];
  }

  async #read(): Promise<void> {
    if (this.#disposed) return;
    this.#loading++;
    const abort = new AbortController();
    this.#reads.add(abort);
    let worked = false;
    try {
      const read = await this.#deps.source.read(this.#deps.sessionId, abort.signal);
      if (this.#disposed) return;
      if (read === null) {
        // Not live: there is nothing to judge a kill against, nothing to show and nothing to try again.
        this.#buffer = [];
        this.#retry?.();
        this.#retry = null;
        this.#retries = 0;
        return;
      }
      this.#source = read.budgetSource;
      this.#prevention = read.prevention;
      for (const kill of read.kills) this.#see(kill);
      const have = this.#sample?.sampledAt;
      const got = read.sample.sampledAt;
      // A sample that came by the stream while the read was out is newer than what the read saw.
      if (have && (got === null || got < have)) this.#render();
      else this.#take(read.sample);
      worked = true;
      this.#retry?.();
      this.#retry = null;
      this.#retries = 0;
    } catch {
      // The last value stays and what is buffered keeps waiting: the read is tried again, a reconnect starts one too.
      if (!this.#disposed) this.#retryLater();
    } finally {
      this.#reads.delete(abort);
      this.#loading--;
      // Kills that came while the read was out are judged now: the ones the read listed are known, the others are new.
      if (worked && this.#loading === 0 && this.#retry === null) for (const kill of this.#buffer.splice(0)) this.#liveKill(kill);
    }
  }

  /** A failed read is tried again after 1 s, 2 s, 4 s and so on up to 30 s. */
  #retryLater(): void {
    if (this.#retry || !this.#deps.scheduler) return;
    const wait = Math.min(1000 * 2 ** this.#retries, 30_000);
    this.#retries++;
    this.#retry = this.#deps.scheduler.after(wait, () => {
      this.#retry = null;
      this.#reading = this.#read();
    });
  }

  /** A kill the read knows: no toast, no ring. It still reads "ended" for what is left of its minute. */
  #see(kill: MemoryKill): void {
    const key = killKey(kill);
    if (this.#kills.has(key)) return;
    this.#kills.set(key, kill);
    // The server's time sets the end, but a clock that is ahead never stretches the chip past a minute from now.
    this.#endFor(kill, Math.min(kill.killedAt.getTime() + CHIP_ENDED_MS, this.#deps.clock.now() + CHIP_ENDED_MS));
  }

  #endFor(kill: MemoryKill, until: number): void {
    const left = until - this.#deps.clock.now();
    if (left <= 0) return;
    this.#ended.push({ kill, until });
    const cancel = this.#deps.scheduler?.after(left, () => {
      this.#timers.delete(cancel!);
      this.#render();
    });
    if (cancel) this.#timers.add(cancel);
  }

  #take(sample: MemorySample): void {
    if (sample.sessionId !== this.#deps.sessionId) return;
    this.#sample = sample;
    this.#tracker = hasValue(sample) ? settle(this.#tracker, shareOf(sample), this.#deps.clock.now()) : initialTracker();
    this.#render();
  }

  #kill(kill: MemoryKill): void {
    if (kill.sessionId !== this.#deps.sessionId) return;
    if (this.#loading > 0 || this.#retry) this.#buffer.push(kill);
    else this.#liveKill(kill);
  }

  /** A kill not seen before: the chip says it, a toast tells it, the ring turns once. */
  #liveKill(kill: MemoryKill): void {
    const key = killKey(kill);
    if (this.#kills.has(key)) return;
    this.#kills.set(key, kill);
    this.#tracker = releaseHold(this.#tracker);
    this.#endFor(kill, this.#deps.clock.now() + CHIP_ENDED_MS);
    this.#signal = key;
    this.#addToast(kill);
    this.#render();
  }

  #addToast(kill: MemoryKill): void {
    const toast: Toast = { kill, cancel: null, until: 0, holds: new Set(), remaining: TOAST_MS };
    this.#toasts.unshift(toast);
    this.#run(toast);
  }

  #run(toast: Toast): void {
    toast.until = this.#deps.clock.now() + toast.remaining;
    toast.cancel = this.#deps.scheduler?.after(toast.remaining, () => this.dismiss(killKey(toast.kill))) ?? null;
  }

  /** Hover or focus stops a toast's clock; `reason` names what holds it. */
  hold(key: string, reason: string): void {
    const toast = this.#toasts.find((t) => killKey(t.kill) === key);
    if (!toast) return;
    if (toast.holds.size === 0) {
      toast.cancel?.();
      toast.cancel = null;
      toast.remaining = Math.max(0, toast.until - this.#deps.clock.now());
    }
    toast.holds.add(reason);
  }

  release(key: string, reason: string): void {
    const toast = this.#toasts.find((t) => killKey(t.kill) === key);
    if (!toast || !toast.holds.delete(reason) || toast.holds.size > 0) return;
    this.#run(toast);
  }

  dismiss(key: string): void {
    const at = this.#toasts.findIndex((t) => killKey(t.kill) === key);
    if (at < 0) return;
    this.#toasts[at]!.cancel?.();
    this.#toasts.splice(at, 1);
    this.#render();
  }

  /** The toast's Details: opens the popover, which lists the kill. */
  async details(key: string): Promise<void> {
    this.dismiss(key);
    await this.open();
  }

  /** Esc: closes the popover first, then dismisses the toast that has the focus. True when it did something. */
  escape(): boolean {
    if (this.#open) {
      this.close();
      return true;
    }
    const focused = this.#toasts.find((t) => t.holds.has("focus"));
    if (!focused) return false;
    this.dismiss(killKey(focused.kill));
    return true;
  }

  #render(): void {
    const now = this.#deps.clock.now();
    this.#ended = this.#ended.filter((e) => e.until > now);
    const ended = this.#ended.reduce<MemoryKill | null>((latest, e) => (latest === null || e.kill.killedAt > latest.killedAt ? e.kill : latest), null);
    // The ring belongs to the ended window: it goes with it.
    if (ended === null) this.#signal = null;
    const view = memoryView(this.#sample, this.#tracker.level, ended);
    if (view === null) this.#open = false;
    const popover = view && this.#open ? this.#popover(view) : null;
    const toasts = this.#toasts.slice(0, MAX_TOASTS).map(toastView);
    const more = Math.max(0, this.#toasts.length - MAX_TOASTS);
    const before = this.viewModel;
    const same = sameView(view, before.view) && samePopover(popover, before.popover) && sameToasts(toasts, before.toasts) && more === before.more && this.#signal === before.signal;
    if (same) return;
    this.set({ view: sameView(view, before.view) ? before.view : view, popover, toasts, more, signal: this.#signal });
  }

  #popover(view: MemoryView): PopoverView {
    const sample = this.#sample!;
    const time = this.#deps.time ?? ((at: Date) => at.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" }));
    const kills = [...this.#kills.values()].sort((a, b) => b.killedAt.getTime() - a.killedAt.getTime());
    const budget = formatBudget(sample.budgetBytes);
    const p = this.#prevention;
    return {
      title: `Memory · ${formatShown(sample.rssBytes)} of ${budget}${WORDS[this.#tracker.level]}`,
      state: view.state,
      fillPercent: view.fillPercent,
      processes: String(sample.processes),
      heaviest: sample.heaviest ? { command: sample.heaviest.command, rss: formatShown(sample.heaviest.rssBytes) } : null,
      budget: `${budget}, fixed when the session started${this.#source ? ` · ${SOURCES[this.#source]}` : ""}`,
      prevention: p ? `${plural(p.testWorkers, "test worker")} · ${p.heapMb} MB heap` : "",
      kills: kills.map((k) => ({ key: killKey(k), time: time(k.killedAt), command: k.command, rss: formatShown(k.rssBytes), signal: k.signal })),
      killsNote: kills.length > 0 ? "" : this.#tracker.level === "over" ? AGENT_OVER : "Nothing yet.",
    };
  }
}
