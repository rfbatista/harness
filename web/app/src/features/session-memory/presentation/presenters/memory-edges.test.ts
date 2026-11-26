// The chip, popover and toast at the edges the stories name: hysteresis and the hold through the presenter, the
// ended window, the toast seen-set under replay, a failed read and a reconnect, the stack and its timer, and
// that no full command line reaches any view.

import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { GB, MB, wireKill, wireRead, wireSample } from "../../../../shared/testing/memory-fixtures";
import { MemoryKillDto, MemoryReadDto, MemorySampleDto } from "../../../../shared/infrastructure/dto/memory-dto";
import type { MemoryRead } from "../../../../shared/kernel/memory/ports";
import type { MemoryKill, MemorySample } from "../../../../shared/kernel/memory/sample";
import { fakeSource, fakeStream, manualClock, manualScheduler } from "../../testing/doubles";
import { ChipPresenter } from "./chip-presenter";

const T0 = Date.parse("2026-11-25T22:40:00Z");
const sample = (share: number, over: Partial<MemorySample> = {}): MemorySample => ({
  sessionId: "s1", taskId: "t1", sampledAt: new Date(T0), rssBytes: share * 4 * GB, budgetBytes: 4 * GB, processes: 9,
  heaviest: { pid: 1, command: "node jest", rssBytes: 864 * MB }, ...over,
});
const kill = (over: Partial<MemoryKill> = {}): MemoryKill => ({
  sessionId: "s1", taskId: "t1", killedAt: new Date(T0), pid: 81234, command: "node jest", rssBytes: 3.5 * GB,
  treeRssBytes: 4.4 * GB, budgetBytes: 4 * GB, signal: "SIGTERM", ...over,
});
const read = (kills: MemoryKill[] = [], s = sample(0.3)): MemoryRead => ({ sample: s, kills, budgetSource: "global", prevention: { heapMb: 1365, testWorkers: 2 } });

function world(initial: MemoryRead | null | Error = read()) {
  const source = fakeSource(initial);
  const stream = fakeStream();
  const clock = manualClock(T0);
  const scheduler = manualScheduler(clock);
  const presenter = new ChipPresenter({ source, stream, clock, scheduler, sessionId: "s1", live: true, time: () => "22:40" });
  return { source, stream, clock, scheduler, presenter };
}
const state = (p: ChipPresenter) => p.viewModel.view?.state;
const later = (w: ReturnType<typeof world>, ms: number) => w.scheduler.advance(ms);

describe("chip hysteresis and hold through the presenter (stories 2, 3)", () => {
  test("79.9 % is normal, 80 % is near", async () => {
    const w = world();
    await w.presenter.start();
    w.stream.sample(sample(0.799));
    assert.equal(state(w.presenter), "normal");
    w.stream.sample(sample(0.8));
    assert.equal(state(w.presenter), "near");
  });

  test("near holds at 75 % and leaves below it, only after 3 s", async () => {
    const w = world(read([], sample(0.85)));
    await w.presenter.start();
    assert.equal(state(w.presenter), "near");
    later(w, 3000);
    w.stream.sample(sample(0.75));
    assert.equal(state(w.presenter), "near", "75 % is not below 75 %");
    w.stream.sample(sample(0.749));
    assert.equal(state(w.presenter), "normal");
  });

  test("99.9 % is near, 100 % is over, and over needs the hold from the last change", async () => {
    const w = world(read([], sample(0.85)));
    await w.presenter.start();
    later(w, 2999);
    w.stream.sample(sample(1.0));
    assert.equal(state(w.presenter), "near", "1 ms short of the hold");
    later(w, 1);
    w.stream.sample(sample(0.999));
    assert.equal(state(w.presenter), "near");
    w.stream.sample(sample(1.0));
    assert.equal(state(w.presenter), "over");
  });

  test("over holds at 95 % and leaves below it, to near", async () => {
    const w = world(read([], sample(1.1)));
    await w.presenter.start();
    later(w, 3000);
    w.stream.sample(sample(0.95));
    assert.equal(state(w.presenter), "over");
    w.stream.sample(sample(0.949));
    assert.equal(state(w.presenter), "near");
  });

  test("the near and over words say it in words, normal says nothing", async () => {
    const w = world(read([], sample(0.3)));
    await w.presenter.start();
    assert.equal(w.presenter.viewModel.view?.word, "");
    later(w, 3000);
    w.stream.sample(sample(0.85));
    assert.equal(w.presenter.viewModel.view?.word, "near budget");
    later(w, 3000);
    w.stream.sample(sample(1.05));
    assert.equal(w.presenter.viewModel.view?.word, "over budget");
  });

  test("the sample that carries the same text does not notify (calm DOM, story 4)", async () => {
    const w = world(read([], sample(0.5)));
    await w.presenter.start();
    let n = 0;
    w.presenter.subscribe(() => n++);
    n = 0;
    w.stream.sample(sample(0.5, { processes: 9, sampledAt: new Date(T0 + 1000) }));
    w.stream.sample(sample(0.5001));
    assert.equal(n, 0);
  });
});

describe("rounding on the chip near 1 GB (story 4)", () => {
  test("just below 1 GB reads in MB, from 1 GB in GB", async () => {
    const w = world(read([], sample(1023.4 / 4096)));
    await w.presenter.start();
    assert.equal(w.presenter.viewModel.view?.num, "1023 MB");
    w.stream.sample(sample(0.25));
    assert.equal(w.presenter.viewModel.view?.num, "1.0");
  });
});

describe("no chip (story 9)", () => {
  test("a read that rejects on the first read leaves no chip, and the first sample brings it", async () => {
    const w = world(new Error("500"));
    await w.presenter.start();
    assert.equal(w.presenter.viewModel.view, null);
    w.stream.sample(sample(0.3));
    assert.equal(w.presenter.viewModel.view?.num, "1.2");
  });

  test("the popover cannot open without a chip, so there is no popover on a 404 or a null sample", async () => {
    const a = world(null);
    await a.presenter.start();
    await a.presenter.open();
    assert.equal(a.presenter.viewModel.popover, null);
    const b = world(read([], sample(0, { sampledAt: null, processes: 0, heaviest: null })));
    await b.presenter.start();
    await b.presenter.toggle();
    assert.equal(b.presenter.viewModel.popover, null);
  });

  test("a read at rss 0 does not paint 0 / 4 GB", async () => {
    const w = world(read([], sample(0, { processes: 0, heaviest: null })));
    await w.presenter.start();
    assert.equal(w.presenter.viewModel.view, null);
  });

  test("an ended session sends rss 0 and the chip, its popover and its ended word go with it", async () => {
    const w = world();
    await w.presenter.start();
    w.stream.kill(kill({ killedAt: new Date(T0 + 1) }));
    await w.presenter.open();
    assert.equal(state(w.presenter), "ended");
    w.stream.sample(sample(0, { processes: 0, heaviest: null }));
    assert.equal(w.presenter.viewModel.view, null);
    assert.equal(w.presenter.viewModel.popover, null);
  });
});

describe("the chip's ended window (story 8)", () => {
  test("a live kill ends the chip for exactly 60 s, whatever the server's timestamp says", async () => {
    const w = world();
    await w.presenter.start();
    later(w, 10_000);
    w.stream.kill(kill({ killedAt: new Date(T0) }));
    later(w, 59_999);
    assert.equal(state(w.presenter), "ended");
    later(w, 1);
    assert.notEqual(state(w.presenter), "ended");
  });

  test("a second kill inside the window shows the newest command and extends the window", async () => {
    const w = world();
    await w.presenter.start();
    w.stream.kill(kill({ killedAt: new Date(T0) }));
    later(w, 30_000);
    w.stream.kill(kill({ pid: 2, command: "pytest", killedAt: new Date(T0 + 30_000) }));
    assert.equal(w.presenter.viewModel.view?.word, "pytest ended");
    later(w, 59_999);
    assert.equal(state(w.presenter), "ended");
    later(w, 1);
    assert.notEqual(state(w.presenter), "ended");
  });

  test("after the window the state is back to what the samples say", async () => {
    const w = world(read([], sample(0.85)));
    await w.presenter.start();
    w.stream.kill(kill({ killedAt: new Date(T0 + 1) }));
    later(w, 60_000);
    assert.equal(state(w.presenter), "near");
    assert.equal(w.presenter.viewModel.view?.word, "near budget");
  });
});

describe("reconnect (story 15)", () => {
  test("the last value stays through a reconnect whose re-read is 404, and through one whose read is slow", async () => {
    const w = world(read([], sample(0.5)));
    await w.presenter.start();
    w.source.answer = null;
    w.stream.resync();
    await w.presenter.settled();
    assert.equal(w.presenter.viewModel.view?.num, "2.0");
    let release!: () => void;
    w.source.gate = new Promise((r) => (release = r));
    w.source.answer = read([], sample(0.6));
    w.stream.resync();
    assert.equal(w.presenter.viewModel.view?.num, "2.0");
    release();
    await w.presenter.settled();
    assert.equal(w.presenter.viewModel.view?.num, "2.4");
  });

  test("every reconnect re-reads, and the stream is still followed once", async () => {
    const w = world();
    await w.presenter.start();
    for (let i = 0; i < 3; i++) {
      w.stream.resync();
      await w.presenter.settled();
    }
    assert.equal(w.source.reads, 4);
    assert.equal(w.stream.followers, 1);
  });

  test("the popover that was open stays open, with the re-read's kills", async () => {
    const w = world();
    await w.presenter.start();
    await w.presenter.open();
    w.source.answer = read([kill({ killedAt: new Date(T0 - 120_000) })]);
    w.stream.resync();
    await w.presenter.settled();
    assert.equal(w.presenter.viewModel.popover?.kills.length, 1);
  });
});

describe("the toast seen-set (story 7)", () => {
  test("a kill seen in the read, then replayed live, toasts nothing", async () => {
    const old = kill({ killedAt: new Date(T0 - 5000) });
    const w = world(read([old]));
    await w.presenter.start();
    w.stream.kill(old);
    assert.deepEqual(w.presenter.viewModel.toasts, []);
  });

  test("a replay arriving during a read that then fails still toasts once, and a second copy does not", async () => {
    const w = world(new Error("500"));
    let release!: () => void;
    w.source.gate = new Promise((r) => (release = r));
    const starting = w.presenter.start();
    const replayed = kill({ killedAt: new Date(T0 - 5000) });
    w.stream.kill(replayed);
    assert.deepEqual(w.presenter.viewModel.toasts, [], "held while the read is out");
    release();
    await starting;
    assert.equal(w.presenter.viewModel.toasts.length, 1, "a failed read seeds nothing, so the kill is not lost");
    w.stream.kill(replayed);
    assert.equal(w.presenter.viewModel.toasts.length, 1, "and is never shown twice");
  });

  test("a failed re-read keeps what the first read seeded: its replay does not toast", async () => {
    const old = kill({ killedAt: new Date(T0 - 5000) });
    const w = world(read([old]));
    await w.presenter.start();
    w.source.answer = new Error("offline");
    w.stream.resync();
    w.stream.kill(old);
    await w.presenter.settled();
    assert.deepEqual(w.presenter.viewModel.toasts, []);
  });

  test("a kill that happened during the reconnect gap and is in the re-read has no toast", async () => {
    const w = world();
    await w.presenter.start();
    const gap = kill({ pid: 7, killedAt: new Date(T0 + 4000) });
    w.source.answer = read([gap]);
    w.stream.resync();
    await w.presenter.settled();
    w.stream.kill(gap);
    assert.deepEqual(w.presenter.viewModel.toasts, []);
  });

  test("the same pid killed at another time is a different kill and toasts", async () => {
    const w = world();
    await w.presenter.start();
    w.stream.kill(kill({ killedAt: new Date(T0 + 1000) }));
    w.stream.kill(kill({ killedAt: new Date(T0 + 2000) }));
    assert.equal(w.presenter.viewModel.toasts.length, 2);
  });

  test("a kill after a dismissed toast does not bring the dismissed one back", async () => {
    const w = world();
    await w.presenter.start();
    const first = kill({ killedAt: new Date(T0 + 1000) });
    w.stream.kill(first);
    w.presenter.dismiss(w.presenter.viewModel.toasts[0]!.key);
    w.stream.kill(first);
    assert.deepEqual(w.presenter.viewModel.toasts, []);
  });
});

describe("toast stack and timer (story 7)", () => {
  test("three kills: two toasts and a summary of one more; the third in order of age", async () => {
    const w = world();
    await w.presenter.start();
    for (const pid of [1, 2, 3]) w.stream.kill(kill({ pid, command: `p${pid}`, killedAt: new Date(T0 + pid) }));
    assert.equal(w.presenter.viewModel.toasts.length, 2);
    assert.equal(w.presenter.viewModel.more, 1);
    w.presenter.dismiss(w.presenter.viewModel.toasts[0]!.key);
    assert.equal(w.presenter.viewModel.more, 0, "the summary line goes when no toast is folded");
    assert.equal(w.presenter.viewModel.toasts.length, 2, "the folded one comes up");
  });

  test("the 12 s are per toast, counted from when it appeared", async () => {
    const w = world();
    await w.presenter.start();
    w.stream.kill(kill({ pid: 1, killedAt: new Date(T0 + 1) }));
    later(w, 5000);
    w.stream.kill(kill({ pid: 2, killedAt: new Date(T0 + 2) }));
    later(w, 7000);
    assert.equal(w.presenter.viewModel.toasts.length, 1, "the first is gone at 12 s");
    later(w, 5000);
    assert.equal(w.presenter.viewModel.toasts.length, 0);
  });

  test("hover pauses at once, and what is left is what runs on after the release", async () => {
    const w = world();
    await w.presenter.start();
    w.stream.kill(kill({ killedAt: new Date(T0 + 1) }));
    const key = w.presenter.viewModel.toasts[0]!.key;
    later(w, 4000);
    w.presenter.hold(key, "hover");
    later(w, 3_600_000);
    assert.equal(w.presenter.viewModel.toasts.length, 1);
    w.presenter.release(key, "hover");
    later(w, 7999);
    assert.equal(w.presenter.viewModel.toasts.length, 1);
    later(w, 1);
    assert.equal(w.presenter.viewModel.toasts.length, 0);
  });

  test("focus alone pauses it, and a release of a reason it was not held for does nothing", async () => {
    const w = world();
    await w.presenter.start();
    w.stream.kill(kill({ killedAt: new Date(T0 + 1) }));
    const key = w.presenter.viewModel.toasts[0]!.key;
    w.presenter.hold(key, "focus");
    w.presenter.release(key, "hover");
    later(w, 60_000);
    assert.equal(w.presenter.viewModel.toasts.length, 1);
    w.presenter.release(key, "focus");
    later(w, 12_000);
    assert.equal(w.presenter.viewModel.toasts.length, 0);
  });

  test("a toast that went on its own leaves the chip saying ended until its own minute is out", async () => {
    const w = world();
    await w.presenter.start();
    w.stream.kill(kill({ killedAt: new Date(T0 + 1) }));
    later(w, 12_000);
    assert.equal(w.presenter.viewModel.toasts.length, 0);
    assert.equal(state(w.presenter), "ended");
  });

  test("Details on a toast folded out of sight does nothing, and one on a shown toast opens a fresh read", async () => {
    const w = world();
    await w.presenter.start();
    w.stream.kill(kill({ killedAt: new Date(T0 + 1) }));
    const reads = w.source.reads;
    await w.presenter.details("no-such-key");
    assert.equal(w.source.reads, reads + 1, "it opens the popover anyway: the person asked for the details");
    assert.notEqual(w.presenter.viewModel.popover, null);
  });
});

describe("no full command line anywhere (constraints)", () => {
  const SECRET = "--token=hunter2 /Users/me/proj/node_modules/.bin/jest --runInBand";

  test("a wire field beyond the contract's command is dropped by the decoders", () => {
    const s = MemorySampleDto.fromJSON(wireSample({ heaviest: { pid: 1, command: "node jest", rss_bytes: 10, cmdline: SECRET } })).toDomain();
    const k = MemoryKillDto.fromJSON(wireKill({ command_line: SECRET, cmdline: SECRET })).toDomain();
    const r = MemoryReadDto.fromJSON(wireRead({ cmdline: SECRET, kills: [wireKill({ args: SECRET })] }));
    assert.doesNotMatch(JSON.stringify([s, k, r]), /hunter2|runInBand/);
  });

  test("every string the chip, popover and toasts show comes from the contract's command only", async () => {
    const s = MemorySampleDto.fromJSON(wireSample({ rss_bytes: Math.round(3.4 * GB), heaviest: { pid: 1, command: "node jest", rss_bytes: 10 * MB, cmdline: SECRET } })).toDomain();
    const k = MemoryKillDto.fromJSON(wireKill({ cmdline: SECRET })).toDomain();
    const w = world(read([], { ...s, sessionId: "s1" }));
    await w.presenter.start();
    w.stream.kill({ ...k, sessionId: "s1" });
    await w.presenter.open();
    assert.doesNotMatch(JSON.stringify(w.presenter.viewModel), /hunter2|runInBand|\/Users\//);
    assert.match(JSON.stringify(w.presenter.viewModel), /node jest/);
  });
});
