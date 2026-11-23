import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { memoryBoardSource } from "../../infrastructure/memory-board-source";
import { cameraKey, positionsKey, storageBoardPreferences } from "../../infrastructure/storage-board-preferences";
import { memoryStorage } from "../../testing/memory-storage";
import { AGENT_NAMES, T0, session } from "../../testing/fixtures";
import { BoardPresenter } from "./board-presenter";

const architect = session({ role: "architect" });
const a = session({ role: "delegate", mode: "implement", parentSessionId: architect.id });
const sessions = [architect, a];

async function board(storage = memoryStorage(), viewport: [number, number] | null = [1000, 600]) {
  const presenter = new BoardPresenter({ source: memoryBoardSource(sessions), preferences: storageBoardPreferences(storage), ticketId: "t1", projectId: "p1", agentNames: AGENT_NAMES, taskTitle: "", now: () => new Date(T0) });
  await presenter.start();
  if (viewport) presenter.setViewport(viewport[0], viewport[1]);
  return presenter;
}

describe("camera", () => {
  test("fits the whole board in the viewport the first time, never past 100 percent", async () => {
    const p = await board(memoryStorage(), [4000, 3000]);
    assert.equal(p.viewModel.camera.scale, 1);
    const small = await board(memoryStorage(), [300, 200]);
    assert.ok(small.viewModel.camera.scale < 1);
    const { camera, canvas } = small.viewModel;
    assert.ok(canvas.w * camera.scale <= 300 && canvas.h * camera.scale <= 200);
  });

  test("panning moves the camera by the pointer's travel", async () => {
    const p = await board();
    const { x, y } = p.viewModel.camera;
    p.panBy(30, -12);
    assert.deepEqual([p.viewModel.camera.x, p.viewModel.camera.y], [x + 30, y - 12]);
  });

  test("zooming keeps the point under the pointer fixed", async () => {
    const p = await board();
    p.actualSize();
    const at = { x: 400, y: 250 };
    const { x, y, scale } = p.viewModel.camera;
    const boardPoint = { x: (at.x - x) / scale, y: (at.y - y) / scale };
    p.zoomAt(1.5, at.x, at.y);
    const next = p.viewModel.camera;
    assert.ok(Math.abs(boardPoint.x * next.scale + next.x - at.x) < 1e-6);
    assert.ok(Math.abs(boardPoint.y * next.scale + next.y - at.y) < 1e-6);
  });

  test("zoom is held between 30 and 200 percent and shown as a percentage", async () => {
    const p = await board();
    for (let i = 0; i < 20; i++) p.zoomAt(2, 0, 0);
    assert.equal(p.viewModel.camera.scale, 2);
    assert.equal(p.viewModel.zoomPercent, 200);
    for (let i = 0; i < 20; i++) p.zoomAt(0.5, 0, 0);
    assert.equal(p.viewModel.zoomPercent, 30);
  });

  test("100 percent gives scale 1 and Fit frames the board again", async () => {
    const p = await board(memoryStorage(), [300, 200]);
    const fitted = p.viewModel.camera;
    p.actualSize();
    assert.equal(p.viewModel.camera.scale, 1);
    p.panBy(500, 500);
    p.fit();
    assert.deepEqual(p.viewModel.camera, fitted);
  });

  test("is kept per task in storage and restored", async () => {
    const storage = memoryStorage();
    const p = await board(storage);
    p.zoomAt(0.5, 0, 0);
    p.panBy(20, 20);
    const kept = p.viewModel.camera;
    assert.ok(storage.entries.has(cameraKey("t1")));
    assert.deepEqual((await board(storage)).viewModel.camera, kept);
    assert.equal(storage.entries.has(cameraKey("t2")), false);
  });

  test("malformed stored data falls back to Fit", async () => {
    for (const text of ["{", JSON.stringify({ x: 1, y: 1, scale: 9 }), JSON.stringify({ x: "a" }), "7"]) {
      const p = await board(memoryStorage({ [cameraKey("t1")]: text }), [300, 200]);
      assert.ok(p.viewModel.camera.scale < 1);
    }
  });

  test("a card drag at 50 percent zoom keeps its place in board space, not the screen delta", async () => {
    const p = await board(memoryStorage(), null);
    p.setViewport(8000, 6000);
    p.actualSize();
    p.zoomAt(0.5, 0, 0);
    assert.equal(p.viewModel.camera.scale, 0.5);
    const start = p.viewModel.cards.find((c) => c.id === a.id)!.rect;
    p.beginDrag(a.id);
    p.dragTo(a.id, 100, 60);
    p.endDrag(a.id);
    const end = p.viewModel.cards.find((c) => c.id === a.id)!.rect;
    assert.deepEqual([end.x - start.x, end.y - start.y], [200, 120]);
  });

  test("a stored camera does not stop the drag memory, and keys do not collide", () => {
    assert.notEqual(cameraKey("t1"), positionsKey("t1"));
  });
});
