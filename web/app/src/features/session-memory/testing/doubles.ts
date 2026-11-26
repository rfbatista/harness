// Doubles for the session memory feature's ports.

import type { MemoryHandlers, MemoryRead, MemorySource, MemoryStream } from "../../../shared/kernel/memory/ports";
import type { MemoryKill, MemorySample } from "../../../shared/kernel/memory/sample";

export interface FakeSource extends MemorySource {
  reads: number;
  /** While set, a read waits for it before answering. */
  gate: Promise<void> | null;
  /** What the next read answers: a read, null for 404, or an error to reject with. */
  answer: MemoryRead | null | Error;
}

export function fakeSource(answer: MemoryRead | null | Error = null): FakeSource {
  const source: FakeSource = {
    reads: 0,
    gate: null,
    answer,
    async read() {
      source.reads++;
      if (source.gate) await source.gate;
      if (source.answer instanceof Error) throw source.answer;
      return source.answer;
    },
  };
  return source;
}

export interface FakeStream extends MemoryStream {
  followers: number;
  sample(sample: MemorySample): void;
  kill(kill: MemoryKill): void;
  resync(): void;
}

export function fakeStream(): FakeStream {
  const handlers = new Set<MemoryHandlers>();
  const stream: FakeStream = {
    followers: 0,
    follow(_sessionId, h) {
      handlers.add(h);
      stream.followers++;
      return () => {
        handlers.delete(h);
        stream.followers--;
      };
    },
    sample: (s) => handlers.forEach((h) => h.onSample(s)),
    kill: (k) => handlers.forEach((h) => h.onKill(k)),
    resync: () => handlers.forEach((h) => h.onResync()),
  };
  return stream;
}

export function manualClock(start = Date.parse("2026-11-25T22:40:00Z")) {
  let now = start;
  return { now: () => now, advance: (ms: number) => void (now += ms) };
}

/** Timers on the manual clock: advancing it runs what falls due, in order. */
export function manualScheduler(clock: ReturnType<typeof manualClock>) {
  let tasks: { at: number; fn: () => void }[] = [];
  return {
    after(ms: number, fn: () => void): () => void {
      const task = { at: clock.now() + ms, fn };
      tasks.push(task);
      return () => void (tasks = tasks.filter((t) => t !== task));
    },
    advance(ms: number): void {
      const end = clock.now() + ms;
      for (;;) {
        const due = tasks.filter((t) => t.at <= end).sort((a, b) => a.at - b.at)[0];
        if (!due) break;
        tasks = tasks.filter((t) => t !== due);
        clock.advance(Math.max(0, due.at - clock.now()));
        due.fn();
      }
      clock.advance(end - clock.now());
    },
    pending: () => tasks.length,
  };
}
