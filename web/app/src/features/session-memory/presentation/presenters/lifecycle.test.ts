import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { GB, MB } from "../../../../shared/testing/memory-fixtures";
import type { MemoryRead } from "../../../../shared/kernel/memory/ports";
import type { MemoryKill, MemorySample } from "../../../../shared/kernel/memory/sample";
import { fakeSource, fakeStream, manualClock, manualScheduler } from "../../testing/doubles";
import { ChipPresenter } from "./chip-presenter";

const T0 = Date.parse("2026-11-25T22:40:00Z");
const sample = (gb: number): MemorySample => ({ sessionId: "s1", taskId: "t1", sampledAt: new Date(T0), rssBytes: gb * GB, budgetBytes: 4 * GB, processes: 9, heaviest: { pid: 1, command: "node jest", rssBytes: 864 * MB } });
const kill = (over: Partial<MemoryKill> = {}): MemoryKill => ({ sessionId: "s1", taskId: "t1", killedAt: new Date(T0), pid: 81234, command: "node jest", rssBytes: 3.5 * GB, treeRssBytes: 4.4 * GB, budgetBytes: 4 * GB, signal: "SIGTERM", ...over });
const read = (kills: MemoryKill[] = []): MemoryRead => ({ sample: sample(1.2), kills, budgetSource: "repository", prevention: { heapMb: 1365, testWorkers: 2 } });

function world(answer: MemoryRead | null | Error = read()) {
  const source = fakeSource(answer);
  const stream = fakeStream();
  const clock = manualClock(T0);
  const scheduler = manualScheduler(clock);
  const presenter = new ChipPresenter({ source, stream, clock, scheduler, sessionId: "s1", live: true });
  return { source, stream, clock, scheduler, presenter };
}

describe("a failed read does not judge buffered kills (F2)", () => {
  test("the first read fails, the hub replays an old kill: no toast, and the read that finally works lists it: still none", async () => {
    const old = kill({ killedAt: new Date(T0 - 120_000) });
    const w = world(new Error("500"));
    await w.presenter.start();
    w.stream.kill(old);
    assert.deepEqual(w.presenter.viewModel.toasts, []);
    w.source.answer = read([old]);
    w.scheduler.advance(1000);
    await w.presenter.settled();
    assert.equal(w.presenter.viewModel.view?.num, "1.2", "the retry brings the chip too");
    assert.deepEqual(w.presenter.viewModel.toasts, []);
  });

  test("the read that finally works does not list the kill: it was new, and toasts then", async () => {
    const w = world(new Error("500"));
    await w.presenter.start();
    w.stream.kill(kill({ killedAt: new Date(T0 + 1) }));
    assert.deepEqual(w.presenter.viewModel.toasts, []);
    w.source.answer = read();
    w.scheduler.advance(1000);
    await w.presenter.settled();
    assert.equal(w.presenter.viewModel.toasts.length, 1);
  });

  test("it retries with a growing backoff while the read fails, and stops once it works", async () => {
    const w = world(new Error("500"));
    await w.presenter.start();
    assert.equal(w.source.reads, 1);
    w.scheduler.advance(1000);
    await w.presenter.settled();
    assert.equal(w.source.reads, 2);
    w.scheduler.advance(1999);
    assert.equal(w.source.reads, 2, "the second wait is longer");
    w.scheduler.advance(1);
    await w.presenter.settled();
    assert.equal(w.source.reads, 3);
    w.source.answer = read();
    w.scheduler.advance(4000);
    await w.presenter.settled();
    const reads = w.source.reads;
    w.scheduler.advance(120_000);
    assert.equal(w.source.reads, reads, "no more retries after a read that worked");
  });

  test("a failed re-read after a reconnect keeps its replays buffered too", async () => {
    const old = kill({ killedAt: new Date(T0 - 5000) });
    const w = world(read());
    await w.presenter.start();
    w.source.answer = new Error("offline");
    w.stream.resync();
    await w.presenter.settled();
    w.stream.kill(old);
    assert.deepEqual(w.presenter.viewModel.toasts, []);
    w.source.answer = read([old]);
    w.scheduler.advance(1000);
    await w.presenter.settled();
    assert.deepEqual(w.presenter.viewModel.toasts, []);
  });

  test("what is still buffered when the island goes is dropped", async () => {
    const w = world(new Error("500"));
    await w.presenter.start();
    w.stream.kill(kill());
    w.presenter.dispose();
    w.source.answer = read();
    w.scheduler.advance(60_000);
    assert.deepEqual(w.presenter.viewModel.toasts, []);
  });
});

describe("the ring clears with the ended window (F3)", () => {
  test("the signal is set for the kill and gone when the 60 s are over, so a static outline never outstays it", async () => {
    const w = world();
    await w.presenter.start();
    w.stream.kill(kill({ killedAt: new Date(T0 + 1) }));
    assert.notEqual(w.presenter.viewModel.signal, null);
    w.scheduler.advance(59_999);
    assert.notEqual(w.presenter.viewModel.signal, null);
    w.scheduler.advance(1);
    assert.equal(w.presenter.viewModel.signal, null);
  });
});

describe("dispose stops everything the presenter started (F4)", () => {
  test("no toast timer, ended timer or retry is left, and the read in flight is aborted", async () => {
    const w = world();
    await w.presenter.start();
    w.stream.kill(kill({ killedAt: new Date(T0 + 1) }));
    assert.ok(w.scheduler.pending() >= 2, "a toast timer and an ended timer");
    w.presenter.dispose();
    assert.equal(w.scheduler.pending(), 0);

    const slow = world();
    let release!: () => void;
    slow.source.gate = new Promise((r) => (release = r));
    const starting = slow.presenter.start();
    slow.presenter.dispose();
    assert.equal(slow.source.signals[0]?.aborted, true);
    release();
    await starting;
    assert.equal(slow.presenter.viewModel.view, null, "a read that lands after dispose changes nothing");
  });

  test("a presenter that is disposed while retrying stops retrying", async () => {
    const w = world(new Error("500"));
    await w.presenter.start();
    w.presenter.dispose();
    assert.equal(w.scheduler.pending(), 0);
  });
});
