import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { memoryBoardSource } from "../../infrastructure/memory-board-source";
import { positionsKey, storageBoardPreferences } from "../../infrastructure/storage-board-preferences";
import { memoryStorage } from "../../testing/memory-storage";
import { AGENT_NAMES, T0, session } from "../../testing/fixtures";
import { BoardPresenter } from "./board-presenter";

const architect = session({ role: "architect" });
const a = session({ role: "delegate", mode: "implement", parentSessionId: architect.id });
const b = session({ role: "delegate", mode: "implement", parentSessionId: architect.id });
const sessions = [architect, a, b];

async function board(storage = memoryStorage(), ticketId = "t1") {
  const presenter = new BoardPresenter({
    source: memoryBoardSource(sessions),
    preferences: storageBoardPreferences(storage),
    ticketId,
    projectId: "p1",
    agentNames: AGENT_NAMES,
    taskTitle: "",
    now: () => new Date(T0),
  });
  await presenter.start();
  return presenter;
}

const rectOf = (p: BoardPresenter, id: string) => p.viewModel.cards.find((c) => c.id === id)!.rect;

describe("dragging a card", () => {
  test("moves that card by the pointer's travel and no other card", async () => {
    const p = await board();
    const before = p.viewModel.cards.map((c) => c.rect);
    const start = rectOf(p, a.id);
    p.beginDrag(a.id);
    p.dragTo(a.id, 120, 40);
    p.endDrag(a.id);
    assert.equal(rectOf(p, a.id).x, start.x + 120);
    assert.equal(rectOf(p, a.id).y, start.y + 40);
    p.viewModel.cards.forEach((c, i) => c.id === a.id || assert.deepEqual(c.rect, before[i]));
  });

  test("keeps the place per task in storage and a new presenter restores it", async () => {
    const storage = memoryStorage();
    const p = await board(storage);
    p.beginDrag(a.id);
    p.dragTo(a.id, 200, 100);
    p.endDrag(a.id);
    const moved = rectOf(p, a.id);
    assert.ok(storage.entries.has(positionsKey("t1")));
    assert.ok(storage.entries.get(positionsKey("t1"))!.includes(a.id));
    assert.deepEqual(rectOf(await board(storage), a.id), moved);
    // another task's board does not see it
    assert.notDeepEqual(rectOf(await board(storage, "t2"), a.id), moved);
    assert.equal(storage.entries.has(positionsKey("t2")), false);
  });

  test("the You card cannot be dragged", async () => {
    const p = await board();
    const before = rectOf(p, "you");
    p.beginDrag("you");
    p.dragTo("you", 50, 50);
    p.endDrag("you");
    assert.deepEqual(rectOf(p, "you"), before);
    assert.equal(p.viewModel.placedCount, 0);
  });

  test("a card is marked dragging only while it is dragged", async () => {
    const p = await board();
    p.beginDrag(a.id);
    assert.equal(p.viewModel.cards.find((c) => c.id === a.id)?.dragging, true);
    p.endDrag(a.id);
    assert.equal(p.viewModel.cards.find((c) => c.id === a.id)?.dragging, false);
  });

  test("a stored place for a session that is gone is ignored", async () => {
    const storage = memoryStorage({ [positionsKey("t1")]: JSON.stringify({ positions: { gone: { x: 5, y: 5 } } }) });
    const p = await board(storage);
    assert.equal(p.viewModel.placedCount, 0);
  });

  test("unreadable or malformed storage falls back to the computed layout", async () => {
    for (const text of ["{", "[]", JSON.stringify({ positions: { [a.id]: { x: "left" } } }), "null"]) {
      const p = await board(memoryStorage({ [positionsKey("t1")]: text }));
      assert.equal(p.viewModel.placedCount, 0);
      assert.equal(p.viewModel.phase, "ready");
    }
    const throwing = { getItem: () => { throw new Error("blocked"); }, setItem: () => { throw new Error("blocked"); }, removeItem: () => { throw new Error("blocked"); } };
    const p = await board(throwing as never);
    p.beginDrag(a.id);
    p.dragTo(a.id, 10, 10);
    p.endDrag(a.id);
    assert.equal(p.viewModel.placedCount, 1);
  });
});

describe("Tidy", () => {
  test("shows how many cards are placed and is disabled when none is", async () => {
    const p = await board();
    assert.deepEqual([p.viewModel.placedCount, p.viewModel.tidyEnabled], [0, false]);
    p.beginDrag(a.id);
    p.dragTo(a.id, 10, 10);
    p.endDrag(a.id);
    p.beginDrag(b.id);
    p.dragTo(b.id, 10, 10);
    p.endDrag(b.id);
    assert.deepEqual([p.viewModel.placedCount, p.viewModel.tidyEnabled], [2, true]);
  });

  test("drops every placed position, returns the computed layout and forgets the storage", async () => {
    const storage = memoryStorage();
    const computed = (await board()).viewModel.cards.map((c) => c.rect);
    const p = await board(storage);
    for (const id of [a.id, b.id]) {
      p.beginDrag(id);
      p.dragTo(id, 90, 90);
      p.endDrag(id);
    }
    p.tidy();
    assert.deepEqual(p.viewModel.cards.map((c) => c.rect), computed);
    assert.equal(p.viewModel.placedCount, 0);
    assert.equal(storage.entries.has(positionsKey("t1")), false);
  });

  test("the board grows to hold a card dragged past its edge", async () => {
    const p = await board();
    const before = p.viewModel.canvas;
    p.beginDrag(a.id);
    p.dragTo(a.id, 2000, 1000);
    p.endDrag(a.id);
    assert.ok(p.viewModel.canvas.w > before.w + 1000);
    assert.ok(p.viewModel.canvas.h > before.h + 500);
  });
});
