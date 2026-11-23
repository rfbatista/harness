import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { memoryBoardSource } from "../../infrastructure/memory-board-source";
import { storageBoardPreferences } from "../../infrastructure/storage-board-preferences";
import { memoryStorage } from "../../testing/memory-storage";
import type { ReviewRequest, TaskMessage } from "../../domain/message";
import type { BoardSession } from "../../domain/session";
import { AGENT_NAMES, T0, message, review, session } from "../../testing/fixtures";
import { BoardPresenter } from "./board-presenter";

const NOW = new Date(T0 + 3_600_000);

const architect = session({ role: "architect", agentId: "arch" });
const impl = session({ role: "delegate", mode: "implement", parentSessionId: architect.id });

/** A scheduler the test runs by hand. */
function manualClock() {
  const timers: { fn: () => void; ms: number; live: boolean }[] = [];
  return {
    schedule: (fn: () => void, ms: number) => {
      const timer = { fn, ms, live: true };
      timers.push(timer);
      return () => void (timer.live = false);
    },
    run: () => timers.splice(0).forEach((t) => t.live && t.fn()),
  };
}

async function board(sessions: BoardSession[] = [architect, impl], opts: { viewport?: [number, number]; storage?: ReturnType<typeof memoryStorage> } = {}) {
  const source = memoryBoardSource(sessions);
  const clock = manualClock();
  const presenter = new BoardPresenter({
    source,
    preferences: storageBoardPreferences(opts.storage ?? memoryStorage()),
    ticketId: "t1",
    projectId: "p1",
    agentNames: AGENT_NAMES,
    taskTitle: "",
    now: () => NOW,
    schedule: clock.schedule,
  });
  await presenter.start();
  if (opts.viewport) presenter.setViewport(...opts.viewport);
  presenter.follow();
  return { presenter, source, clock };
}

const card = (p: BoardPresenter, id: string) => p.viewModel.cards.find((c) => c.id === id);
const settle = () => new Promise((resolve) => setImmediate(resolve));

describe("a session arrives", () => {
  test("takes its computed slot, rings once, and is announced; no existing card moves", async () => {
    const { presenter, source, clock } = await board();
    const before = presenter.viewModel.cards.map((c) => [c.id, c.rect] as const);
    const qa = session({ role: "delegate", mode: "qa", agentId: "rev", parentSessionId: architect.id });
    source.emit({ kind: "session", session: qa });
    assert.ok(card(presenter, qa.id));
    assert.equal(card(presenter, qa.id)?.ringed, true);
    assert.equal(presenter.viewModel.announcement, "code-reviewer started");
    for (const [id, rect] of before) assert.deepEqual(card(presenter, id)?.rect, rect);
    clock.run();
    assert.equal(card(presenter, qa.id)?.ringed, false);
  });

  test("a session that opens a column does not shift the cards after it: they stay where they were drawn until Tidy", async () => {
    const sub = session({ role: "delegate", mode: "test", parentSessionId: impl.id });
    const { presenter, source } = await board([architect, impl, sub]);
    const drawn = presenter.viewModel.cards.map((c) => [c.id, c.rect] as const);
    const qa = session({ role: "delegate", mode: "qa", parentSessionId: architect.id });
    source.emit({ kind: "session", session: qa });
    for (const [id, rect] of drawn) assert.deepEqual(card(presenter, id)?.rect, rect);
    // The new column's slot is where the held card sits: the arrival takes the first free spot below it.
    const held = card(presenter, sub.id)!.rect;
    const landed = card(presenter, qa.id)!.rect;
    assert.ok(landed.y >= held.y + held.h || landed.x >= held.x + held.w || landed.x + landed.w <= held.x);
    presenter.tidy();
    assert.notDeepEqual(card(presenter, sub.id)?.rect, drawn.find(([id]) => id === sub.id)?.[1]);
  });

  test("a resync that brings a new column does not shift the cards after it", async () => {
    const sub = session({ role: "delegate", mode: "test", parentSessionId: impl.id });
    const { presenter, source } = await board([architect, impl, sub]);
    const drawn = presenter.viewModel.cards.map((c) => [c.id, c.rect] as const);
    source.setSessions([architect, impl, sub, session({ role: "delegate", mode: "qa", parentSessionId: architect.id })]);
    source.setStatus("resynced");
    await settle();
    for (const [id, rect] of drawn) assert.deepEqual(card(presenter, id)?.rect, rect);
  });

  test("a deleted session does not shift the cards after it", async () => {
    const first = session({ role: "delegate", mode: "implement", parentSessionId: architect.id });
    const second = session({ role: "delegate", mode: "implement", parentSessionId: architect.id });
    const { presenter, source } = await board([architect, first, second]);
    const drawn = card(presenter, second.id)!.rect;
    source.emit({ kind: "session-deleted", id: first.id });
    assert.deepEqual(card(presenter, second.id)?.rect, drawn);
  });

  test("an event for another task changes nothing", async () => {
    const { presenter, source } = await board();
    source.emit({ kind: "session", session: session({ ticketId: "other" }) });
    assert.equal(presenter.viewModel.cards.length, 3);
  });

  test("never moves a dragged card: a covered computed slot moves down its column to the first free spot", async () => {
    const { presenter, source } = await board();
    const first = session({ role: "delegate", mode: "review", parentSessionId: architect.id });
    source.emit({ kind: "session", session: first });
    // The person drags the implementer onto the slot where the next review session will be computed.
    const slot = card(presenter, first.id)!.rect;
    presenter.beginDrag(impl.id);
    const from = card(presenter, impl.id)!.rect;
    presenter.dragTo(impl.id, slot.x - from.x, slot.y + slot.h + 24 - from.y);
    presenter.endDrag(impl.id);
    const second = session({ role: "delegate", mode: "review", parentSessionId: architect.id });
    source.emit({ kind: "session", session: second });
    const dragged = card(presenter, impl.id)!.rect;
    const landed = card(presenter, second.id)!.rect;
    assert.deepEqual(dragged, { ...from, x: slot.x, y: slot.y + slot.h + 24 });
    assert.equal(landed.x, slot.x);
    assert.equal(landed.y, dragged.y + dragged.h + 24);
    // The arrival's place is its own now: nothing moves it again.
    source.emit({ kind: "session", session: { ...second, status: "done" } });
    assert.deepEqual(card(presenter, second.id)?.rect, landed);
  });

  test("an off-screen arrival shows the chip and leaves the camera; Show pans to it", async () => {
    const { presenter, source } = await board([architect, impl], { viewport: [500, 400] });
    presenter.actualSize();
    const camera = presenter.viewModel.camera;
    const late = session({ role: "delegate", mode: "ship", agentId: "rev", parentSessionId: architect.id });
    source.emit({ kind: "session", session: late });
    assert.equal(presenter.viewModel.arrival?.text, "code-reviewer arrived · Show");
    assert.deepEqual(presenter.viewModel.camera, camera);
    presenter.showArrival();
    assert.equal(presenter.viewModel.arrival, null);
    const r = card(presenter, late.id)!.rect;
    const { camera: c } = presenter.viewModel;
    const midX = (r.x + r.w / 2) * c.scale + c.x;
    assert.ok(Math.abs(midX - 250) < 1);
  });

  test("an arrival in view shows no chip", async () => {
    const { presenter, source } = await board([architect, impl], { viewport: [4000, 3000] });
    source.emit({ kind: "session", session: session({ parentSessionId: architect.id, role: "delegate" }) });
    assert.equal(presenter.viewModel.arrival, null);
  });
});

describe("a status changes", () => {
  test("changes the card in place with a ring and an announcement; ending dims it; resuming undims it", async () => {
    const { presenter, source } = await board();
    const rect = card(presenter, impl.id)!.rect;
    source.emit({ kind: "session", session: { ...impl, status: "done" } });
    assert.equal(card(presenter, impl.id)?.dimmed, true);
    assert.equal(card(presenter, impl.id)?.ringed, true);
    assert.deepEqual(card(presenter, impl.id)?.rect, rect);
    assert.equal(presenter.viewModel.announcement, "frontend-developer: done");
    source.emit({ kind: "session", session: { ...impl, status: "running" } });
    assert.equal(card(presenter, impl.id)?.dimmed, false);
    assert.equal(presenter.viewModel.announcement, "frontend-developer resumed");
  });

  test("an upsert that changes nothing visible rings nothing", async () => {
    const { presenter, source } = await board();
    source.emit({ kind: "session", session: { ...impl, updatedAt: new Date(T0 + 99) } });
    assert.equal(card(presenter, impl.id)?.ringed, false);
    assert.equal(presenter.viewModel.announcement, "");
  });

  test("a deleted session leaves the board", async () => {
    const { presenter, source } = await board();
    source.emit({ kind: "session-deleted", id: impl.id });
    assert.equal(card(presenter, impl.id), undefined);
  });
});

describe("a message lands", () => {
  const sent = (kind: TaskMessage["kind"] = "status_report", extra: Partial<TaskMessage> = {}) => message(impl.id, architect.id, { kind, createdAt: new Date(NOW.getTime() - 60_000), ...extra });

  test("pulses the edge once, updates the badge and announces who said what", async () => {
    const { presenter, source, clock } = await board();
    const m = sent("review_request");
    source.emit({ kind: "message", message: m });
    const edge = presenter.viewModel.edges.find((e) => e.to === impl.id)!;
    assert.equal(edge.pulsing, true);
    assert.equal(edge.badge?.count, 1);
    assert.equal(presenter.viewModel.announcement, "frontend-developer sent software-architect a review request");
    clock.run();
    assert.equal(presenter.viewModel.edges.find((e) => e.to === impl.id)?.pulsing, false);
  });

  test("the delivery event of the same message pulses nothing and counts it once", async () => {
    const { presenter, source, clock } = await board();
    const m = sent();
    source.emit({ kind: "message", message: m });
    clock.run();
    source.emit({ kind: "message", message: { ...m } });
    const edge = presenter.viewModel.edges.find((e) => e.to === impl.id)!;
    assert.equal(edge.pulsing, false);
    assert.equal(edge.badge?.count, 1);
  });

  test("a message already in the first read does not pulse when its delivery event comes", async () => {
    const m = sent();
    const source = memoryBoardSource([architect, impl]);
    source.setMessages([m]);
    const presenter = new BoardPresenter({ source, preferences: storageBoardPreferences(memoryStorage()), ticketId: "t1", projectId: "p1", agentNames: AGENT_NAMES, taskTitle: "", now: () => NOW });
    await presenter.start();
    presenter.follow();
    source.emit({ kind: "message", message: { ...m } });
    assert.equal(presenter.viewModel.edges.find((e) => e.to === impl.id)?.pulsing, false);
  });

  test("a question keeps the signal tone until a reply arrives", async () => {
    const { presenter, source } = await board();
    const q = sent("question");
    source.emit({ kind: "message", message: q });
    assert.equal(presenter.viewModel.edges.find((e) => e.to === impl.id)?.tone, "signal");
    source.emit({ kind: "message", message: message(architect.id, impl.id, { kind: "reply", inReplyTo: q.id }) });
    assert.equal(presenter.viewModel.edges.find((e) => e.to === impl.id)?.tone, "started");
  });

  test("a message of another task changes nothing", async () => {
    const { presenter, source } = await board();
    source.emit({ kind: "message", message: sent("question", { taskId: "other" }) });
    assert.equal(presenter.viewModel.edges.find((e) => e.to === impl.id)?.badge, null);
  });
});

describe("a review request to the person", () => {
  test("turns the You edge amber, pulses it and announces it; answering it clears it", async () => {
    const { presenter, source } = await board();
    const r: ReviewRequest = review(architect.id, { createdAt: new Date(NOW.getTime() - 120_000) });
    source.emit({ kind: "review", review: r });
    const edge = presenter.viewModel.edges.find((e) => e.from === "you" && e.to === architect.id)!;
    assert.equal(edge.tone, "amber");
    assert.equal(edge.pulsing, true);
    assert.equal(presenter.viewModel.announcement, "A review request waits on you");
    source.emit({ kind: "review", review: { ...r, state: "approved" } });
    assert.equal(presenter.viewModel.edges.find((e) => e.from === "you" && e.to === architect.id)?.tone, "started");
  });
});

describe("the feed drops and comes back", () => {
  test("keeps the picture while paused; on resync re-reads the seed and rings only what changed", async () => {
    const { presenter, source } = await board();
    source.setStatus("paused");
    assert.equal(presenter.viewModel.feed, "paused");
    assert.equal(presenter.viewModel.cards.length, 3);
    const before = presenter.viewModel.cards.map((c) => [c.id, c.rect] as const);
    const extra = session({ role: "delegate", mode: "qa", parentSessionId: architect.id });
    source.setSessions([architect, { ...impl, status: "done" }, extra]);
    source.setStatus("resynced");
    await settle();
    assert.equal(presenter.viewModel.feed, "resynced");
    assert.equal(card(presenter, impl.id)?.ringed, true);
    assert.equal(card(presenter, extra.id)?.ringed, true);
    assert.equal(card(presenter, architect.id)?.ringed, false);
    for (const [id, rect] of before) assert.deepEqual(card(presenter, id)?.rect, rect);
  });

  test("stop stops following", async () => {
    const { presenter, source } = await board();
    presenter.stop();
    assert.equal(source.followers(), 0);
  });
});
