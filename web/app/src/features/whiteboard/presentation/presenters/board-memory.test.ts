import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { memoryBoardSource } from "../../infrastructure/memory-board-source";
import { storageBoardPreferences } from "../../infrastructure/storage-board-preferences";
import { memoryStorage } from "../../testing/memory-storage";
import type { BoardSession } from "../../domain/session";
import { AGENT_NAMES, T0, memoryKill, memorySample, session } from "../../testing/fixtures";
import { BoardPresenter } from "./board-presenter";

let now = new Date(T0 + 3_600_000);
const at = (offsetMs: number) => now.getTime() + offsetMs;

function manualScheduler() {
  let elapsed = 0;
  let timers: { fn: () => void; due: number; live: boolean }[] = [];
  return {
    schedule: (fn: () => void, ms: number) => {
      const timer = { fn, due: elapsed + ms, live: true };
      timers.push(timer);
      return () => void (timer.live = false);
    },
    /** Moves the clock by `ms` and runs what falls due. */
    advance(ms: number) {
      elapsed += ms;
      now = new Date(now.getTime() + ms);
      const due = timers.filter((t) => t.due <= elapsed);
      timers = timers.filter((t) => t.due > elapsed);
      for (const t of due) if (t.live) t.fn();
    },
  };
}

const architect = session({ role: "architect", agentId: "arch" });

async function board(sessions: BoardSession[]) {
  now = new Date(T0 + 3_600_000);
  const source = memoryBoardSource(sessions);
  const clock = manualScheduler();
  const presenter = new BoardPresenter({ source, preferences: storageBoardPreferences(memoryStorage()), ticketId: "t1", projectId: "p1", agentNames: AGENT_NAMES, taskTitle: "", now: () => now, schedule: clock.schedule });
  await presenter.start();
  presenter.follow();
  return { presenter, source, clock };
}
const card = (p: BoardPresenter, id: string) => p.viewModel.cards.find((c) => c.id === id)!;

describe("memory line of a live session card", () => {
  test("a session with a sample in the payload shows the track, the number, and the word when it is not normal", async () => {
    const calm = session({ role: "delegate", parentSessionId: architect.id });
    const hot = session({ role: "delegate", parentSessionId: architect.id });
    const { presenter } = await board([architect, { ...calm, memory: memorySample(calm.id, 1.2) }, { ...hot, memory: memorySample(hot.id, 3.4) }]);
    const a = card(presenter, calm.id).memory!;
    assert.deepEqual([a.state, a.num, a.of, a.word, a.badge], ["normal", "1.2", " / 4 GB", "", ""]);
    const b = card(presenter, hot.id).memory!;
    assert.deepEqual([b.state, b.num, b.word, b.fillPercent, b.badge], ["near", "3.4", "near budget", 85, "near budget"]);
  });

  test("no line before a first sample, at rss 0, for an ended session or for You", async () => {
    const none = session({ role: "delegate", parentSessionId: architect.id });
    const first = session({ role: "delegate", parentSessionId: architect.id, memory: null });
    const zero = session({ role: "delegate", parentSessionId: architect.id });
    const done = session({ role: "delegate", parentSessionId: architect.id, status: "done" });
    const { presenter } = await board([architect, none, first, { ...zero, memory: memorySample(zero.id, 0, { processes: 0, heaviest: null }) }, { ...done, memory: memorySample(done.id, 2) }]);
    for (const s of [none, first, zero, done]) assert.equal(card(presenter, s.id).memory, null, s.id);
    assert.equal(card(presenter, "you").memory, null);
  });

  test("the feed's samples change the line; the first sample of a session brings it", async () => {
    const s = session({ role: "delegate", parentSessionId: architect.id });
    const { presenter, source } = await board([architect, s]);
    assert.equal(card(presenter, s.id).memory, null);
    source.emit({ kind: "memory", sample: memorySample(s.id, 1.2) });
    assert.equal(card(presenter, s.id).memory?.num, "1.2");
    source.emit({ kind: "memory", sample: memorySample(s.id, 1.4) });
    assert.equal(card(presenter, s.id).memory?.num, "1.4");
    source.emit({ kind: "memory", sample: memorySample(s.id, 0, { processes: 0, heaviest: null }) });
    assert.equal(card(presenter, s.id).memory, null, "the last sample of a session that ends clears the line");
  });

  test("a sample of a session the task does not have is ignored", async () => {
    const { presenter, source } = await board([architect]);
    source.emit({ kind: "memory", sample: memorySample("elsewhere", 3) });
    assert.equal(presenter.viewModel.cards.filter((c) => c.memory).length, 0);
  });

  test("the state waits out the hold, like the chip", async () => {
    const s = session({ role: "delegate", parentSessionId: architect.id });
    const { presenter, source, clock } = await board([architect, { ...s, memory: memorySample(s.id, 3.4) }]);
    source.emit({ kind: "memory", sample: memorySample(s.id, 4.4) });
    assert.equal(card(presenter, s.id).memory?.state, "near");
    clock.advance(3_000);
    source.emit({ kind: "memory", sample: memorySample(s.id, 4.4) });
    assert.equal(card(presenter, s.id).memory?.state, "over");
  });

  test("a session change that carries no memory keeps the last sample; a newer one replaces it", async () => {
    const s = session({ role: "delegate", parentSessionId: architect.id, memory: null });
    const { presenter, source } = await board([architect, s]);
    source.emit({ kind: "memory", sample: memorySample(s.id, 1.5) });
    source.emit({ kind: "session", session: { ...s, status: "thinking", memory: null } });
    assert.equal(card(presenter, s.id).memory?.num, "1.5");
    source.emit({ kind: "session", session: { ...s, status: "thinking", memory: memorySample(s.id, 2.5, {}, at(10_000)) } });
    assert.equal(card(presenter, s.id).memory?.num, "2.5");
    source.emit({ kind: "session", session: { ...s, status: "thinking", memory: memorySample(s.id, 1.0, {}, at(-10_000)) } });
    assert.equal(card(presenter, s.id).memory?.num, "2.5", "an older sample does not roll it back");
  });

  test("the card's accessible name carries the memory sentence", async () => {
    const s = session({ role: "delegate", parentSessionId: architect.id });
    const { presenter } = await board([architect, { ...s, memory: memorySample(s.id, 3.4) }]);
    assert.match(card(presenter, s.id).name, /Memory: 3\.4 GB of the 4 GB budget · near budget · 7 processes$/);
  });
});

describe("a process ended on a card", () => {
  async function ended() {
    const s = session({ role: "delegate", parentSessionId: architect.id });
    const other = session({ role: "delegate", parentSessionId: architect.id });
    const world = await board([architect, { ...s, memory: memorySample(s.id, 3.4) }, other]);
    const before = world.presenter.viewModel.cards.map((c) => [c.id, c.rect] as const);
    world.source.emit({ kind: "memory-killed", kill: memoryKill(s.id, at(0)) });
    return { ...world, s, other, before };
  }

  test("for 5 minutes it reads ended with its age, one ring, and nothing moves", async () => {
    const { presenter, clock, s, before } = await ended();
    const m = card(presenter, s.id).memory!;
    assert.deepEqual([m.state, m.ended, m.badge], ["ended", { command: "node jest", age: "now" }, "process ended"]);
    assert.equal(card(presenter, s.id).memoryRinged, true);
    assert.equal(card(presenter, s.id).attention, false, "the attention wash is for waiting on you");
    for (const [id, rect] of before) assert.deepEqual(card(presenter, id).rect, rect);
    clock.advance(2_400);
    assert.equal(card(presenter, s.id).memoryRinged, false);
  });

  test("the age follows the clock, then the samples come back after 5 minutes", async () => {
    const { presenter, clock, s } = await ended();
    clock.advance(120_000);
    assert.equal(card(presenter, s.id).memory?.ended?.age, "2m");
    clock.advance(179_999);
    assert.equal(card(presenter, s.id).memory?.state, "ended");
    clock.advance(1);
    assert.equal(card(presenter, s.id).memory?.ended, null);
    assert.equal(card(presenter, s.id).memory?.num, "3.4");
  });

  test("a kill of a session the task does not have, or of an ended one, draws nothing", async () => {
    const { presenter, source } = await ended();
    source.emit({ kind: "memory-killed", kill: memoryKill("elsewhere", at(0)) });
    assert.equal(presenter.viewModel.cards.filter((c) => c.memory?.state === "ended").length, 1);
  });
});

describe("the bar's memory summary", () => {
  const delegate = () => session({ role: "delegate", parentSessionId: architect.id });

  test("sums the last values of the live sessions, with one badge per state present", async () => {
    const [a, b, c] = [delegate(), delegate(), delegate()];
    const { presenter, source } = await board([architect, { ...a, memory: memorySample(a.id, 3.4) }, { ...b, memory: memorySample(b.id, 1.2) }, { ...c, memory: memorySample(c.id, 6.1, { budgetBytes: 8 * 1024 ** 3 }) }]);
    const m = presenter.viewModel.memory!;
    assert.deepEqual([m.live, m.total, m.near, m.ended], [4, "10.7 GB", 1, 0]);
    source.emit({ kind: "memory-killed", kill: memoryKill(c.id, at(0)) });
    assert.equal(presenter.viewModel.memory?.ended, 1);
  });

  test("kills count for 5 minutes and then drop; several kills count each", async () => {
    const a = delegate();
    const { presenter, source, clock } = await board([architect, { ...a, memory: memorySample(a.id, 1.2) }]);
    source.emit({ kind: "memory-killed", kill: memoryKill(a.id, at(0)) });
    source.emit({ kind: "memory-killed", kill: memoryKill(a.id, at(1), { pid: 2 }) });
    assert.equal(presenter.viewModel.memory?.ended, 2);
    clock.advance(299_999);
    assert.equal(presenter.viewModel.memory?.ended, 2);
    clock.advance(1);
    assert.equal(presenter.viewModel.memory?.ended, 0);
  });

  test("no summary while no live session has a sample; ended sessions and the sessions without one do not count", async () => {
    const calm = delegate();
    const done = session({ role: "delegate", parentSessionId: architect.id, status: "done" });
    const { presenter, source } = await board([architect, calm, { ...done, memory: memorySample(done.id, 5) }]);
    const bar = () => presenter.viewModel.memory;
    assert.equal(bar(), null);
    source.emit({ kind: "memory", sample: memorySample(calm.id, 2) });
    assert.deepEqual([bar()?.live, bar()?.total], [2, "2.0 GB"]);
  });

  test("a session that goes away leaves the sum", async () => {
    const [a, b] = [delegate(), delegate()];
    const { presenter, source } = await board([architect, { ...a, memory: memorySample(a.id, 2) }, { ...b, memory: memorySample(b.id, 1) }]);
    assert.equal(presenter.viewModel.memory?.total, "3.0 GB");
    source.emit({ kind: "session-deleted", id: b.id });
    assert.equal(presenter.viewModel.memory?.total, "2.0 GB");
  });

  test("the canvas's accessible name carries the same phrase", async () => {
    const a = delegate();
    const { presenter, source } = await board([architect, { ...a, memory: memorySample(a.id, 3.4) }]);
    assert.match(presenter.viewModel.canvasLabel, /^2 sessions as a graph\. Memory: 3\.4 GB across 2 live, 1 near budget\. Tab moves/);
    source.emit({ kind: "memory-killed", kill: memoryKill(a.id, at(0)) });
    assert.match(presenter.viewModel.canvasLabel, /Memory: 3\.4 GB across 2 live, 1 near budget, 1 process ended\./);
    const none = await board([architect]);
    assert.doesNotMatch(none.presenter.viewModel.canvasLabel, /Memory/);
  });
});

describe("the card's age and the kills' identity (F5, F6)", () => {
  test("the age advances by itself, minute by minute, while the feed is quiet", async () => {
    const s = session({ role: "delegate", parentSessionId: architect.id });
    const { presenter, source, clock } = await board([architect, { ...s, memory: memorySample(s.id, 1.2) }]);
    source.emit({ kind: "memory-killed", kill: memoryKill(s.id, at(0)) });
    assert.equal(card(presenter, s.id).memory?.ended?.age, "now");
    for (const minute of [1, 2, 3, 4]) {
      clock.advance(60_000);
      assert.equal(card(presenter, s.id).memory?.ended?.age, `${minute}m`);
    }
    clock.advance(60_000);
    assert.equal(card(presenter, s.id).memory?.ended, null, "and the fifth minute ends it");
  });

  test("the same kill delivered twice counts once and keeps its first age", async () => {
    const s = session({ role: "delegate", parentSessionId: architect.id });
    const { presenter, source, clock } = await board([architect, { ...s, memory: memorySample(s.id, 1.2) }]);
    const kill = memoryKill(s.id, at(0));
    source.emit({ kind: "memory-killed", kill });
    clock.advance(120_000);
    source.emit({ kind: "memory-killed", kill });
    assert.equal(presenter.viewModel.memory?.ended, 1);
    assert.equal(card(presenter, s.id).memory?.ended?.age, "2m");
  });
});

describe("the bar's over budget badge (design D7)", () => {
  test("a session over budget counts in its own badge and in the sentence", async () => {
    const [a, b] = [session({ role: "delegate", parentSessionId: architect.id }), session({ role: "delegate", parentSessionId: architect.id })];
    const { presenter } = await board([architect, { ...a, memory: memorySample(a.id, 4.4) }, { ...b, memory: memorySample(b.id, 3.4) }]);
    const m = presenter.viewModel.memory!;
    assert.deepEqual([m.near, m.over], [1, 1]);
    assert.match(m.sentence, /1 near budget, 1 over budget\.$/);
  });
});
