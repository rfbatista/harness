import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { memoryBoardSource } from "../../infrastructure/memory-board-source";
import { storageBoardPreferences } from "../../infrastructure/storage-board-preferences";
import { memoryStorage } from "../../testing/memory-storage";
import { recordingHost } from "../../testing/recording-host";
import type { SessionBranches } from "../../domain/branches";
import type { Rect } from "../../domain/layout";
import type { ReviewRequest, TaskMessage } from "../../domain/message";
import type { BoardSession } from "../../domain/session";
import { AGENT_NAMES, T0, message, review, session } from "../../testing/fixtures";
import { BoardPresenter } from "./board-presenter";

const NOW = new Date(T0 + 3_600_000);
const ago = (minutes: number): Date => new Date(NOW.getTime() - minutes * 60_000);

const architect = session({ role: "architect", agentId: "arch" });
const impl = session({ role: "delegate", mode: "implement", parentSessionId: architect.id });
const impl2 = session({ role: "delegate", mode: "implement", parentSessionId: architect.id });

interface Options {
  sessions?: BoardSession[];
  messages?: TaskMessage[];
  reviews?: ReviewRequest[];
  storage?: ReturnType<typeof memoryStorage>;
}

async function board(opts: Options = {}) {
  const source = memoryBoardSource(opts.sessions ?? [architect, impl, impl2]);
  source.setMessages(opts.messages ?? []);
  source.setReviews(opts.reviews ?? []);
  const host = recordingHost();
  const presenter = new BoardPresenter({ source, preferences: storageBoardPreferences(opts.storage ?? memoryStorage()), host, ticketId: "t1", projectId: "p1", agentNames: AGENT_NAMES, taskTitle: "", now: () => NOW });
  await presenter.start();
  presenter.follow();
  return { presenter, source, host };
}

const rect = (p: BoardPresenter, id: string): Rect => p.viewModel.cards.find((c) => c.id === id)!.rect;
const rects = (p: BoardPresenter) => Object.fromEntries(p.viewModel.cards.map((c) => [c.id, { ...c.rect }]));
const edge = (p: BoardPresenter, to: string) => p.viewModel.edges.find((e) => e.to === to)!;
const overlap = (a: Rect, b: Rect) => a.x < b.x + b.w && b.x < a.x + a.w && a.y < b.y + b.h && b.y < a.y + a.h;

function drag(p: BoardPresenter, id: string, to: { x: number; y: number }) {
  const from = rect(p, id);
  p.beginDrag(id);
  p.dragTo(id, to.x - from.x, to.y - from.y);
  p.endDrag(id);
}

describe("arrival placement", () => {
  test("a session of a new mode, whose column a dragged card sits in, lands without covering it and moves nothing", async () => {
    const { presenter, source } = await board();
    // Where the first "review" column will be: right of the implement column.
    const column = rect(presenter, impl.id);
    drag(presenter, impl2.id, { x: column.x + column.w + 24, y: column.y });
    const before = rects(presenter);
    const reviewer = session({ role: "delegate", mode: "review", parentSessionId: architect.id });
    source.emit({ kind: "session", session: reviewer });
    const after = rects(presenter);
    for (const id of Object.keys(before)) assert.deepEqual(after[id], before[id], `${id} stayed`);
    for (const c of presenter.viewModel.cards) if (c.id !== reviewer.id) assert.equal(overlap(after[reviewer.id]!, c.rect), false, `covers ${c.id}`);
  });

  test("a sub-delegate of a dragged card lands beside the board's cards and the parent stays put", async () => {
    const { presenter, source } = await board();
    drag(presenter, impl.id, { x: 600, y: 500 });
    const before = rects(presenter);
    const sub = session({ role: "delegate", mode: "test", parentSessionId: impl.id });
    source.emit({ kind: "session", session: sub });
    const after = rects(presenter);
    for (const id of Object.keys(before)) assert.deepEqual(after[id], before[id]);
    assert.ok(presenter.viewModel.cards.some((c) => c.id === sub.id));
    assert.ok(presenter.viewModel.edges.some((e) => e.from === impl.id && e.to === sub.id));
  });

  test("arrivals in a row never overlap each other or a dragged card", async () => {
    const { presenter, source } = await board();
    drag(presenter, impl.id, { x: rect(presenter, impl2.id).x, y: rect(presenter, impl2.id).y + 200 });
    for (let i = 0; i < 4; i++) source.emit({ kind: "session", session: session({ role: "delegate", mode: "implement", parentSessionId: architect.id }) });
    const cards = presenter.viewModel.cards.filter((c) => c.kind === "session");
    for (const a of cards) for (const b of cards) if (a.id !== b.id) assert.equal(overlap(a.rect, b.rect), false, `${a.id} covers ${b.id}`);
  });
});

describe("Tidy", () => {
  test("counts each dragged card once, however often it moves", async () => {
    const { presenter } = await board();
    drag(presenter, impl.id, { x: 500, y: 300 });
    drag(presenter, impl.id, { x: 520, y: 310 });
    assert.equal(presenter.viewModel.placedCount, 1);
    drag(presenter, impl2.id, { x: 700, y: 300 });
    assert.equal(presenter.viewModel.placedCount, 2);
    assert.equal(presenter.viewModel.tidyEnabled, true);
  });

  test("after drags and arrivals it gives exactly the layout a fresh board draws, with no card placed", async () => {
    const storage = memoryStorage();
    const { presenter, source } = await board({ storage });
    drag(presenter, impl.id, { x: 640, y: 380 });
    const late = session({ role: "delegate", mode: "review", parentSessionId: architect.id });
    source.emit({ kind: "session", session: late });
    presenter.tidy();
    assert.equal(presenter.viewModel.placedCount, 0);
    assert.equal(presenter.viewModel.tidyEnabled, false);
    const fresh = await board({ sessions: [architect, impl, impl2, late] });
    assert.deepEqual(rects(presenter), rects(fresh.presenter));
    // And a reload does not bring the old places back.
    assert.deepEqual(rects((await board({ sessions: [architect, impl, impl2, late], storage })).presenter), rects(fresh.presenter));
  });
});

describe("thread badge derivation", () => {
  test("the latest message is the newest by time, not the last to arrive", async () => {
    const newer = message(impl.id, architect.id, { kind: "status_report", createdAt: ago(2) });
    const { presenter, source } = await board({ messages: [newer] });
    source.emit({ kind: "message", message: message(architect.id, impl.id, { kind: "reply", createdAt: ago(30) }) });
    const badge = edge(presenter, impl.id).badge!;
    assert.equal(badge.count, 2);
    assert.equal(badge.text, "report 2m");
  });

  test("a pending question waits from the oldest unanswered one, even when a newer message is later in the thread", async () => {
    const q1 = message(impl.id, architect.id, { kind: "question", createdAt: ago(20) });
    const q2 = message(impl.id, architect.id, { kind: "review_request", createdAt: ago(5) });
    const report = message(impl.id, architect.id, { kind: "status_report", createdAt: ago(1) });
    const { presenter } = await board({ messages: [q1, q2, report] });
    assert.equal(edge(presenter, impl.id).tone, "signal");
    assert.equal(edge(presenter, impl.id).badge?.count, 3);
    assert.match(edge(presenter, impl.id).badge!.text, /waits 20m/);
  });

  test("answering the oldest of two leaves the other pending", async () => {
    const q1 = message(impl.id, architect.id, { kind: "question", createdAt: ago(20) });
    const q2 = message(impl.id, architect.id, { kind: "question", createdAt: ago(5) });
    const { presenter, source } = await board({ messages: [q1, q2] });
    source.emit({ kind: "message", message: message(architect.id, impl.id, { kind: "reply", inReplyTo: q1.id, createdAt: ago(1) }) });
    assert.equal(edge(presenter, impl.id).tone, "signal");
    assert.match(edge(presenter, impl.id).badge!.text, /waits 5m/);
  });

  test("two pairs are counted apart", async () => {
    const msgs = [message(impl.id, architect.id), message(impl.id, architect.id), message(architect.id, impl2.id)];
    const { presenter } = await board({ messages: msgs });
    assert.equal(edge(presenter, impl.id).badge?.count, 2);
    assert.equal(edge(presenter, impl2.id).badge?.count, 1);
  });

  test("the delivery event of a review request pulses nothing the second time", async () => {
    const r = review(architect.id, { state: "pending", createdAt: ago(3) });
    const { presenter, source } = await board();
    source.emit({ kind: "review", review: r });
    const you = () => presenter.viewModel.edges.find((e) => e.from === "you")!;
    assert.equal(you().tone, "amber");
    assert.equal(you().badge?.count, 1);
    source.emit({ kind: "review", review: { ...r } });
    assert.equal(you().badge?.count, 1);
  });
});

describe("push state from session_branches", () => {
  const withBranch = session({ branch: "agent/x" });
  const list = (pushes: number, own = true): SessionBranches => ({ sessionId: withBranch.id, branches: [{ name: "agent/x", isSessionBranch: own, state: "session", pushes }] });
  const stateOf = (p: BoardPresenter) => p.viewModel.cards.find((c) => c.id === withBranch.id)!.pushState;

  test("a feed list for a session turns not pushed into pushed, and a list for an unknown session is ignored", async () => {
    const { presenter, source } = await board({ sessions: [architect, withBranch] });
    source.emit({ kind: "branches", branches: list(0) });
    assert.equal(stateOf(presenter), "not pushed");
    source.emit({ kind: "branches", branches: { sessionId: "ghost", branches: list(3).branches } });
    assert.equal(presenter.viewModel.cards.length, 3);
    source.emit({ kind: "branches", branches: list(1) });
    assert.equal(stateOf(presenter), "pushed");
  });

  test("a list with no session branch in it says nothing; an ended session keeps what it pushed", async () => {
    const { presenter, source } = await board({ sessions: [architect, withBranch] });
    source.emit({ kind: "branches", branches: list(2, false) });
    assert.equal(stateOf(presenter), "");
    source.emit({ kind: "branches", branches: list(2) });
    source.emit({ kind: "session", session: { ...withBranch, status: "done" } });
    assert.equal(stateOf(presenter), "pushed");
  });
});

describe("keyboard walk", () => {
  const rev = session({ role: "delegate", mode: "review", agentId: "rev", parentSessionId: architect.id });

  test("from You, Right reaches the architect and Left goes back to nothing further", async () => {
    const { presenter } = await board({ sessions: [architect, impl, rev] });
    presenter.focus("you");
    presenter.key({ key: "ArrowRight" });
    assert.equal(presenter.viewModel.focusedId, architect.id);
    presenter.key({ key: "ArrowLeft" });
    assert.equal(presenter.viewModel.focusedId, "you");
    presenter.key({ key: "ArrowLeft" });
    assert.equal(presenter.viewModel.focusedId, "you");
  });

  test("a key it does not own is left to the browser, with or without a focused card", async () => {
    const { presenter } = await board({ sessions: [architect, impl, rev] });
    assert.equal(presenter.key({ key: "Tab" }), false);
    presenter.focus(impl.id);
    assert.equal(presenter.key({ key: "Tab" }), false);
    assert.equal(presenter.key({ key: "x" }), false);
  });

  test("Escape with nothing selected asks the page to close nothing", async () => {
    const { presenter, host } = await board({ sessions: [architect, impl, rev] });
    presenter.focus(impl.id);
    presenter.key({ key: "Escape" });
    assert.equal(host.calls.includes("close"), false);
    assert.equal(presenter.viewModel.focusedId, null);
  });

  test("the walk follows the board as it changes: a new card is reached, a deleted one is not", async () => {
    const { presenter, source } = await board({ sessions: [architect, impl, rev] });
    presenter.focus(impl.id);
    const extra = session({ role: "delegate", mode: "implement", parentSessionId: architect.id });
    source.emit({ kind: "session", session: extra });
    presenter.key({ key: "ArrowDown" });
    assert.equal(presenter.viewModel.focusedId, extra.id);
    source.emit({ kind: "session-deleted", id: extra.id });
    presenter.focus(impl.id);
    presenter.key({ key: "ArrowDown" });
    assert.equal(presenter.viewModel.focusedId, impl.id);
  });
});

describe("empty and ended states over time", () => {
  test("an empty board becomes live when the first session arrives", async () => {
    const { presenter, source } = await board({ sessions: [] });
    assert.equal(presenter.viewModel.state, "empty");
    source.emit({ kind: "session", session: architect });
    assert.equal(presenter.viewModel.state, "live");
  });

  test("an ended board turns live again when one session resumes, and ended when it ends again", async () => {
    const done = [architect, impl].map((s) => ({ ...s, status: "done" as const }));
    const { presenter, source } = await board({ sessions: done });
    assert.equal(presenter.viewModel.state, "ended");
    source.emit({ kind: "session", session: { ...done[1]!, status: "running" } });
    assert.equal(presenter.viewModel.state, "live");
    source.emit({ kind: "session", session: done[1]! });
    assert.equal(presenter.viewModel.state, "ended");
  });

  test("deleting the last session returns the empty state, with You still drawn", async () => {
    const { presenter, source } = await board({ sessions: [architect] });
    source.emit({ kind: "session-deleted", id: architect.id });
    assert.equal(presenter.viewModel.state, "empty");
    assert.deepEqual(presenter.viewModel.cards.map((c) => c.id), ["you"]);
  });
});
