import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { GB, MB } from "../../../../shared/testing/memory-fixtures";
import type { MemoryRead } from "../../../../shared/kernel/memory/ports";
import type { MemoryKill, MemorySample } from "../../../../shared/kernel/memory/sample";
import { fakeSource, fakeStream, manualClock, manualScheduler } from "../../testing/doubles";
import { ChipPresenter } from "./chip-presenter";

const T0 = Date.parse("2026-11-25T22:40:00Z");
const sample = (gb: number, over: Partial<MemorySample> = {}): MemorySample => ({
  sessionId: "s1", taskId: "t1", sampledAt: new Date(T0), rssBytes: gb * GB, budgetBytes: 4 * GB, processes: 9,
  heaviest: { pid: 1, command: "node jest", rssBytes: 864 * MB }, ...over,
});
const kill = (over: Partial<MemoryKill> = {}): MemoryKill => ({
  sessionId: "s1", taskId: "t1", killedAt: new Date(T0), pid: 81234, command: "node jest", rssBytes: 3.5 * GB,
  treeRssBytes: 4.4 * GB, budgetBytes: 4 * GB, signal: "SIGTERM", ...over,
});
const read = (kills: MemoryKill[] = [], s = sample(1.2)): MemoryRead => ({ sample: s, kills, budgetSource: "repository", prevention: { heapMb: 1365, testWorkers: 2 } });

async function setup(initial: MemoryRead = read()) {
  const source = fakeSource(initial);
  const stream = fakeStream();
  const clock = manualClock(T0);
  const scheduler = manualScheduler(clock);
  const presenter = new ChipPresenter({ source, stream, clock, scheduler, sessionId: "s1", live: true, time: () => "22:40" });
  await presenter.start();
  return { source, stream, clock, scheduler, presenter };
}
const titles = (p: ChipPresenter) => p.viewModel.toasts.map((t) => t.title);

describe("toast on an encerramento por memória", () => {
  test("a live kill toasts with the process, its rss, the tree and the budget; a SIGKILL says it was killed and did not exit", async () => {
    const { presenter, stream } = await setup();
    stream.kill(kill({ killedAt: new Date(T0 + 1000) }));
    const t = presenter.viewModel.toasts[0]!;
    assert.deepEqual([t.title, t.rss, t.tree, t.budget, t.notExited], ["node jest ended: over the memory budget", "3.5 GB", "4.4 GB", "4 GB", false]);
    stream.kill(kill({ pid: 2, command: "pytest", signal: "SIGKILL", killedAt: new Date(T0 + 2000) }));
    const k = presenter.viewModel.toasts[0]!;
    assert.deepEqual([k.title, k.notExited], ["pytest killed: over the memory budget", true]);
  });

  test("samples never toast, near or over", async () => {
    const { presenter, stream } = await setup();
    stream.sample(sample(3.5));
    stream.sample(sample(4.5));
    assert.deepEqual(presenter.viewModel.toasts, []);
  });

  test("a kill of the initial read, or of a re-read, has no toast, and neither does its replay", async () => {
    const old = kill();
    const { presenter, stream, source } = await setup(read([old]));
    stream.kill(old);
    assert.deepEqual(presenter.viewModel.toasts, []);
    // reconnect: the hub replays the old kill and a new one happened while we were away
    const fresh = kill({ pid: 9, killedAt: new Date(T0 + 5000) });
    source.answer = read([old, fresh]);
    stream.resync();
    stream.kill(old);
    stream.kill(fresh);
    await presenter.settled();
    assert.deepEqual(presenter.viewModel.toasts, [], "a re-read seeds the seen set, whatever the replay did meanwhile");
    stream.kill(fresh);
    assert.deepEqual(presenter.viewModel.toasts, [], "and a reconnect never shows one toast twice");
    stream.kill(kill({ pid: 10, killedAt: new Date(T0 + 9000) }));
    assert.equal(presenter.viewModel.toasts.length, 1, "a kill nobody has seen does");
  });

  test("a replay that arrives while the first read is out waits for it", async () => {
    const old = kill();
    const source = fakeSource(read([old]));
    let release!: () => void;
    source.gate = new Promise((r) => (release = r));
    const stream = fakeStream();
    const clock = manualClock(T0);
    const presenter = new ChipPresenter({ source, stream, clock, scheduler: manualScheduler(clock), sessionId: "s1", live: true });
    const starting = presenter.start();
    stream.kill(old);
    assert.deepEqual(presenter.viewModel.toasts, []);
    release();
    await starting;
    assert.deepEqual(presenter.viewModel.toasts, []);
  });

  test("a kill of another session is not shown", async () => {
    const { presenter, stream } = await setup();
    stream.kill(kill({ sessionId: "other" }));
    assert.deepEqual(presenter.viewModel.toasts, []);
  });

  test("a toast lasts 12 s, and hover or focus stops its clock", async () => {
    const { presenter, stream, scheduler } = await setup();
    stream.kill(kill({ killedAt: new Date(T0 + 1) }));
    const key = presenter.viewModel.toasts[0]!.key;
    scheduler.advance(11_999);
    assert.equal(presenter.viewModel.toasts.length, 1);
    scheduler.advance(1);
    assert.equal(presenter.viewModel.toasts.length, 0);

    stream.kill(kill({ pid: 2, killedAt: new Date(T0 + 2) }));
    const key2 = presenter.viewModel.toasts[0]!.key;
    scheduler.advance(5_000);
    presenter.hold(key2, "hover");
    presenter.hold(key2, "focus");
    scheduler.advance(60_000);
    assert.equal(presenter.viewModel.toasts.length, 1, "held");
    presenter.release(key2, "hover");
    scheduler.advance(60_000);
    assert.equal(presenter.viewModel.toasts.length, 1, "still held by focus");
    presenter.release(key2, "focus");
    scheduler.advance(6_999);
    assert.equal(presenter.viewModel.toasts.length, 1, "the 7 s that were left");
    scheduler.advance(1);
    assert.equal(presenter.viewModel.toasts.length, 0);
    assert.notEqual(key, key2);
  });

  test("two toasts at most, the newest first, and the older ones fold into a count", async () => {
    const { presenter, stream } = await setup();
    for (const pid of [1, 2, 3, 4]) stream.kill(kill({ pid, command: `p${pid}`, killedAt: new Date(T0 + pid) }));
    assert.deepEqual(titles(presenter).map((t) => t.split(" ")[0]), ["p4", "p3"]);
    assert.equal(presenter.viewModel.more, 2);
  });

  test("dismissing removes a toast; Details opens the popover on the kill and dismisses its toast", async () => {
    const { presenter, stream } = await setup();
    stream.kill(kill({ killedAt: new Date(T0 + 1) }));
    stream.kill(kill({ pid: 2, command: "pytest", killedAt: new Date(T0 + 2) }));
    presenter.dismiss(presenter.viewModel.toasts[0]!.key);
    assert.deepEqual(titles(presenter).map((t) => t.split(" ")[0]), ["node"]);
    await presenter.details(presenter.viewModel.toasts[0]!.key);
    assert.notEqual(presenter.viewModel.popover, null);
    assert.equal(presenter.viewModel.popover!.kills.length, 2);
    assert.deepEqual(presenter.viewModel.toasts, []);
  });

  test("Esc closes the popover first, then dismisses the toast that has the focus", async () => {
    const { presenter, stream } = await setup();
    stream.kill(kill({ killedAt: new Date(T0 + 1) }));
    const key = presenter.viewModel.toasts[0]!.key;
    presenter.hold(key, "focus");
    await presenter.open();
    assert.equal(presenter.escape(), true);
    assert.equal(presenter.viewModel.popover, null);
    assert.equal(presenter.viewModel.toasts.length, 1, "the toast stays for the next Esc");
    assert.equal(presenter.escape(), true);
    assert.equal(presenter.viewModel.toasts.length, 0);
    assert.equal(presenter.escape(), false, "nothing left for Esc to do");
  });
});

describe("the chip after a kill", () => {
  test("it says the process ended for 60 s, then goes back to samples", async () => {
    const { presenter, stream, scheduler } = await setup();
    stream.kill(kill({ killedAt: new Date(T0 + 1) }));
    assert.deepEqual([presenter.viewModel.view?.state, presenter.viewModel.view?.word], ["ended", "node jest ended"]);
    scheduler.advance(59_999);
    assert.equal(presenter.viewModel.view?.state, "ended");
    scheduler.advance(1);
    assert.equal(presenter.viewModel.view?.state, "normal");
  });

  test("the window runs from when the kill is seen live; a kill of the read counts from its own time", async () => {
    const { presenter, scheduler } = await setup(read([kill({ killedAt: new Date(T0 - 30_000) })]));
    assert.equal(presenter.viewModel.view?.state, "ended");
    scheduler.advance(29_999);
    assert.equal(presenter.viewModel.view?.state, "ended");
    scheduler.advance(1);
    assert.equal(presenter.viewModel.view?.state, "normal");
    const old = await setup(read([kill({ killedAt: new Date(T0 - 61_000) })]));
    assert.equal(old.presenter.viewModel.view?.state, "normal");
  });

  test("a server clock that is ahead never stretches the chip past a minute from the read", async () => {
    const { presenter, scheduler } = await setup(read([kill({ killedAt: new Date(T0 + 600_000) })]));
    assert.equal(presenter.viewModel.view?.state, "ended");
    scheduler.advance(60_000);
    assert.equal(presenter.viewModel.view?.state, "normal");
  });

  test("the signal is set once per live kill and never for a kill of the read", async () => {
    const { presenter, stream } = await setup(read([kill({ killedAt: new Date(T0 - 1000) })]));
    assert.equal(presenter.viewModel.signal, null);
    stream.kill(kill({ pid: 5, killedAt: new Date(T0 + 1) }));
    const signal = presenter.viewModel.signal;
    assert.notEqual(signal, null);
    stream.sample(sample(1.3));
    assert.equal(presenter.viewModel.signal, signal, "samples do not set it again");
  });

  test("the chip keeps the value while ended, the fill at the current value", async () => {
    const { presenter, stream } = await setup(read([], sample(3.4)));
    stream.kill(kill({ killedAt: new Date(T0 + 1) }));
    assert.deepEqual([presenter.viewModel.view?.num, presenter.viewModel.view?.fillPercent], ["3.4", 85]);
  });
});
