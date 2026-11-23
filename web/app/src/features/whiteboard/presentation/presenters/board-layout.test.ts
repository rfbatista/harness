import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { memoryBoardSource } from "../../infrastructure/memory-board-source";
import { AGENT_NAMES, T0, session } from "../../testing/fixtures";
import type { BoardSession } from "../../domain/session";
import { BoardPresenter } from "./board-presenter";

async function board(sessions: BoardSession[], compact = false) {
  const presenter = new BoardPresenter({ source: memoryBoardSource(sessions), ticketId: "t1", projectId: "p1", agentNames: AGENT_NAMES, taskTitle: "Ship it", now: () => new Date(T0 + 3_600_000) });
  await presenter.start();
  if (compact) presenter.setCompact(true);
  return presenter;
}

const rect = (vm: { cards: { id: string; rect: { x: number; y: number } }[] }, id: string) => vm.cards.find((c) => c.id === id)!.rect;

function fleet() {
  const architect = session({ role: "architect", agentId: "arch" });
  const impl1 = session({ role: "delegate", mode: "implement", parentSessionId: architect.id });
  const review = session({ role: "delegate", mode: "review", parentSessionId: architect.id });
  const impl2 = session({ role: "delegate", mode: "implement", parentSessionId: architect.id });
  const sub = session({ role: "delegate", mode: "test", parentSessionId: impl1.id });
  return { architect, impl1, review, impl2, sub };
}

describe("computed layout", () => {
  test("runs You, the architect, a column per delegate mode, then sub-delegates, left to right", async () => {
    const f = fleet();
    const { viewModel: vm } = await board([f.architect, f.impl1, f.review, f.impl2, f.sub]);
    const xs = [rect(vm, "you").x, rect(vm, f.architect.id).x, rect(vm, f.impl1.id).x, rect(vm, f.review.id).x, rect(vm, f.sub.id).x];
    assert.deepEqual([...xs].sort((a, b) => a - b), xs);
    assert.equal(new Set(xs).size, 5);
    assert.equal(rect(vm, f.impl1.id).x, rect(vm, f.impl2.id).x);
  });

  test("mode columns follow the order the modes first appeared, and a new mode appends at the right", async () => {
    const architect = session({ role: "architect" });
    const review = session({ role: "delegate", mode: "review", parentSessionId: architect.id });
    const implement = session({ role: "delegate", mode: "implement", parentSessionId: architect.id });
    const first = (await board([architect, review, implement])).viewModel;
    assert.ok(rect(first, review.id).x < rect(first, implement.id).x);
    const qa = session({ role: "delegate", mode: "qa", parentSessionId: architect.id });
    const grown = (await board([architect, review, implement, qa])).viewModel;
    for (const id of [review.id, implement.id, architect.id, "you"]) assert.deepEqual(rect(grown, id), rect(first, id));
    assert.ok(rect(grown, qa.id).x > rect(grown, implement.id).x);
  });

  test("within a column, sessions run top to bottom in start order", async () => {
    const f = fleet();
    const { viewModel: vm } = await board([f.architect, f.impl2, f.impl1]);
    assert.ok(rect(vm, f.impl1.id).y < rect(vm, f.impl2.id).y);
  });

  test("the same sessions always give the same positions", async () => {
    const f = fleet();
    const all = [f.architect, f.impl1, f.review, f.impl2, f.sub];
    const a = (await board(all)).viewModel;
    const b = (await board([...all].reverse())).viewModel;
    assert.deepEqual(a.cards.map((c) => [c.id, c.rect]).sort(), b.cards.map((c) => [c.id, c.rect]).sort());
  });

  test("a status change moves no card", async () => {
    const f = fleet();
    const source = memoryBoardSource([f.architect, f.impl1, f.review]);
    const presenter = new BoardPresenter({ source, ticketId: "t1", projectId: "p1", agentNames: AGENT_NAMES, taskTitle: "", now: () => new Date(T0) });
    await presenter.start();
    const before = presenter.viewModel.cards.map((c) => c.rect);
    source.setSessions([f.architect, { ...f.impl1, status: "done" }, f.review]);
    await presenter.start();
    assert.deepEqual(presenter.viewModel.cards.map((c) => c.rect), before);
  });

  test("each column has a heading with its count", async () => {
    const f = fleet();
    const { viewModel: vm } = await board([f.architect, f.impl1, f.review, f.impl2, f.sub]);
    assert.deepEqual(
      vm.columns.map((c) => [c.label, c.count]),
      [["Architect", 1], ["implement", 2], ["review", 1], ["Sub-delegates", 1]],
    );
  });

  test("sessions a person started sit under the architect's column, in start order", async () => {
    const architect = session({ role: "architect" });
    const peer = session();
    const { viewModel: vm } = await board([architect, peer]);
    assert.equal(rect(vm, peer.id).x, rect(vm, architect.id).x);
    assert.ok(rect(vm, peer.id).y > rect(vm, architect.id).y);
  });

  test("compact cards take the compact size and the board gets narrower", async () => {
    const f = fleet();
    const wide = (await board([f.architect, f.impl1])).viewModel;
    const narrow = (await board([f.architect, f.impl1], true)).viewModel;
    assert.ok(narrow.cards.find((c) => c.id === f.impl1.id)!.rect.w < wide.cards.find((c) => c.id === f.impl1.id)!.rect.w);
    assert.ok(narrow.canvas.w < wide.canvas.w);
    assert.equal(narrow.compact, true);
  });
});
