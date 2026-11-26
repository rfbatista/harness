import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { memoryBoardSource } from "../../infrastructure/memory-board-source";
import { storageBoardPreferences } from "../../infrastructure/storage-board-preferences";
import { memoryStorage } from "../../testing/memory-storage";
import type { BoardSession } from "../../domain/session";
import { AGENT_NAMES, T0, memoryKill, memorySample, session } from "../../testing/fixtures";
import { BoardPresenter } from "./board-presenter";

let now = new Date(T0 + 3_600_000);

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
const delegate = () => session({ role: "delegate", parentSessionId: architect.id });

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

describe("whiteboard edges: hysteresis and hold on the card (stories 3, 10, 12)", () => {
  test("79.9 % is normal and 80 % is near on the card, with the compact badge", async () => {
    const s = delegate();
    const { presenter, source, clock } = await board([architect, { ...s, memory: memorySample(s.id, 0.799 * 4) }]);
    assert.deepEqual([card(presenter, s.id).memory?.state, card(presenter, s.id).memory?.badge], ["normal", ""]);
    clock.advance(3000);
    source.emit({ kind: "memory", sample: memorySample(s.id, 0.8 * 4) });
    assert.deepEqual([card(presenter, s.id).memory?.state, card(presenter, s.id).memory?.badge], ["near", "near budget"]);
  });

  test("near holds at 75 % and leaves below; over holds at 95 % and leaves below; both after 3 s", async () => {
    const s = delegate();
    const { presenter, source, clock } = await board([architect, { ...s, memory: memorySample(s.id, 3.4) }]);
    clock.advance(3000);
    source.emit({ kind: "memory", sample: memorySample(s.id, 0.75 * 4) });
    assert.equal(card(presenter, s.id).memory?.state, "near");
    source.emit({ kind: "memory", sample: memorySample(s.id, 0.749 * 4) });
    assert.equal(card(presenter, s.id).memory?.state, "normal");
    clock.advance(3000);
    source.emit({ kind: "memory", sample: memorySample(s.id, 4.0) });
    assert.equal(card(presenter, s.id).memory?.state, "over");
    clock.advance(3000);
    source.emit({ kind: "memory", sample: memorySample(s.id, 0.95 * 4) });
    assert.equal(card(presenter, s.id).memory?.state, "over");
    source.emit({ kind: "memory", sample: memorySample(s.id, 0.949 * 4) });
    assert.equal(card(presenter, s.id).memory?.state, "near");
  });

  test("the hold is 3 s to the millisecond on the card", async () => {
    const s = delegate();
    const { presenter, source, clock } = await board([architect, { ...s, memory: memorySample(s.id, 3.4) }]);
    clock.advance(2999);
    source.emit({ kind: "memory", sample: memorySample(s.id, 4.4) });
    assert.equal(card(presenter, s.id).memory?.state, "near");
    clock.advance(1);
    source.emit({ kind: "memory", sample: memorySample(s.id, 4.4) });
    assert.equal(card(presenter, s.id).memory?.state, "over");
    assert.equal(card(presenter, s.id).memory?.badge, "over budget");
  });
});

describe("whiteboard edges: the 5 min ended window on the card (story 11)", () => {
  test("ended at 4 min 59.999 s, samples again at 5 min; ring only once; the card's place and attention untouched", async () => {
    const s = delegate();
    const { presenter, source, clock } = await board([architect, { ...s, memory: memorySample(s.id, 3.4) }]);
    source.emit({ kind: "memory-killed", kill: memoryKill(s.id, now.getTime()) });
    assert.equal(card(presenter, s.id).memoryRinged, true);
    clock.advance(299_999);
    assert.equal(card(presenter, s.id).memory?.state, "ended");
    assert.equal(card(presenter, s.id).memoryRinged, false, "the ring is spent");
    assert.equal(card(presenter, s.id).attention, false);
    clock.advance(1);
    assert.notEqual(card(presenter, s.id).memory?.state, "ended");
  });

  test("a second kill inside the window restarts it for the card and counts twice on the bar", async () => {
    const s = delegate();
    const { presenter, source, clock } = await board([architect, { ...s, memory: memorySample(s.id, 1.2) }]);
    source.emit({ kind: "memory-killed", kill: memoryKill(s.id, now.getTime()) });
    clock.advance(200_000);
    source.emit({ kind: "memory-killed", kill: memoryKill(s.id, now.getTime(), { pid: 2, command: "pytest" }) });
    assert.equal(card(presenter, s.id).memory?.ended?.command, "pytest");
    assert.equal(presenter.viewModel.memory?.ended, 2);
    clock.advance(150_000);
    assert.equal(card(presenter, s.id).memory?.state, "ended", "the first has expired, the second has not");
    assert.equal(presenter.viewModel.memory?.ended, 1);
  });

  test("the ended card says the command and the age, never a command line", async () => {
    const s = delegate();
    const { presenter, source } = await board([architect, { ...s, memory: memorySample(s.id, 3.4) }]);
    source.emit({ kind: "memory-killed", kill: memoryKill(s.id, now.getTime()) });
    const view = card(presenter, s.id);
    assert.match(JSON.stringify([view.memory, view.name]), /node jest/);
    assert.doesNotMatch(JSON.stringify([view.memory, view.name, presenter.viewModel.canvasLabel]), /--|\/Users\//);
  });
});

describe("whiteboard edges: the bar's sum and badges (stories 13, 14)", () => {
  test("the total is one rounding of the sum and moves as a sample moves", async () => {
    const [a, b, c] = [delegate(), delegate(), delegate()];
    const { presenter, source } = await board([architect, { ...a, memory: memorySample(a.id, 0.34) }, { ...b, memory: memorySample(b.id, 0.34) }, { ...c, memory: memorySample(c.id, 0.34) }]);
    assert.equal(presenter.viewModel.memory?.total, "1.0 GB");
    source.emit({ kind: "memory", sample: memorySample(c.id, 1.34, {}, now.getTime() + 1000) });
    assert.equal(presenter.viewModel.memory?.total, "2.0 GB");
  });

  test("the near badge counts each near session and drops a session that left near after the hold", async () => {
    const [a, b] = [delegate(), delegate()];
    const { presenter, source, clock } = await board([architect, { ...a, memory: memorySample(a.id, 3.4) }, { ...b, memory: memorySample(b.id, 3.5) }]);
    assert.equal(presenter.viewModel.memory?.near, 2);
    clock.advance(3000);
    source.emit({ kind: "memory", sample: memorySample(b.id, 1.0) });
    assert.equal(presenter.viewModel.memory?.near, 1);
    assert.match(presenter.viewModel.memory!.sentence, /^Memory: 4\.4 GB across 3 live, 1 near budget\.$/);
  });

  test("a session with a kill and no sample still gets its ended badge on the bar", async () => {
    const a = delegate();
    const { presenter, source } = await board([architect, a]);
    const bar = () => presenter.viewModel.memory;
    assert.equal(bar(), null);
    source.emit({ kind: "memory-killed", kill: memoryKill(a.id, now.getTime()) });
    assert.equal(bar()?.ended, 1);
    assert.equal(bar()?.total, null);
  });

  test("the sentence says singular and plural as the bar's copy does", async () => {
    const a = delegate();
    const { presenter, source } = await board([architect, { ...a, memory: memorySample(a.id, 1.2) }]);
    source.emit({ kind: "memory-killed", kill: memoryKill(a.id, now.getTime()) });
    source.emit({ kind: "memory-killed", kill: memoryKill(a.id, now.getTime(), { pid: 2 }) });
    assert.match(presenter.viewModel.memory!.sentence, /2 processes ended\./);
  });
});
