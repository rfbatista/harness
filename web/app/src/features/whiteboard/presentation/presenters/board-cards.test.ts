import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { memoryBoardSource } from "../../infrastructure/memory-board-source";
import { AGENT_NAMES, T0, session } from "../../testing/fixtures";
import { BoardPresenter } from "./board-presenter";

const NOW = new Date(T0 + 3_600_000);

async function board(sessions = [session()]) {
  const presenter = new BoardPresenter({ source: memoryBoardSource(sessions), ticketId: "t1", projectId: "p1", agentNames: AGENT_NAMES, taskTitle: "Ship it", now: () => NOW });
  await presenter.start();
  return presenter.viewModel;
}

describe("cards and started edges", () => {
  test("has the You card even when the task has no session", async () => {
    const vm = await board([]);
    assert.deepEqual(vm.cards.map((c) => c.id), ["you"]);
    assert.equal(vm.cards[0]?.kind, "you");
  });

  test("draws one card per session after the You card", async () => {
    const architect = session({ role: "architect", agentId: "arch" });
    const delegate = session({ role: "delegate", parentSessionId: architect.id });
    const vm = await board([architect, delegate]);
    assert.deepEqual(vm.cards.map((c) => c.id), ["you", architect.id, delegate.id]);
  });

  test("joins each session to the one that started it, and a session a person started to You", async () => {
    const architect = session({ role: "architect", agentId: "arch" });
    const delegate = session({ role: "delegate", parentSessionId: architect.id });
    const sub = session({ role: "delegate", parentSessionId: delegate.id });
    const peer = session();
    const vm = await board([architect, delegate, sub, peer]);
    assert.deepEqual(
      vm.edges.map((e) => [e.from, e.to]),
      [["you", architect.id], [architect.id, delegate.id], [delegate.id, sub.id], ["you", peer.id]],
    );
  });

  test("a session brought under the architect has the same started edge", async () => {
    const architect = session({ role: "architect" });
    const brought = session({ role: "delegate", parentSessionId: architect.id, broughtUnder: true });
    const vm = await board([architect, brought]);
    assert.ok(vm.edges.some((e) => e.from === architect.id && e.to === brought.id));
    assert.equal(vm.cards.find((c) => c.id === brought.id)?.line?.lead, "brought under");
  });

  test("an ended session keeps its edge, dimmed; a failed one is red, not dimmed", async () => {
    const architect = session({ role: "architect" });
    const ended = session({ role: "delegate", parentSessionId: architect.id, status: "done" });
    const failed = session({ role: "delegate", parentSessionId: architect.id, status: "failed" });
    const vm = await board([architect, ended, failed]);
    const edge = vm.edges.find((e) => e.to === ended.id);
    assert.equal(edge?.dimmed, true);
    assert.equal(vm.cards.find((c) => c.id === ended.id)?.dimmed, true);
    const red = vm.cards.find((c) => c.id === failed.id);
    assert.equal(red?.dimmed, false);
    assert.equal(red?.tone, "danger");
    assert.equal(vm.cards.filter((c) => c.tone === "danger").length, 1);
  });

  test("a card says what the row says: agent, mode, model, branch, state word and chip", async () => {
    const s = session({ agentId: "rev", mode: "review", model: "opus", branch: "agent/x", status: "stopped", interruptedAt: NOW, task: "Review it" });
    const card = (await board([s])).cards[1];
    assert.equal(card?.agent, "code-reviewer");
    assert.equal(card?.mode, "review");
    assert.equal(card?.model, "opus");
    assert.equal(card?.branch, "agent/x");
    assert.equal(card?.word, "interrupted by restart");
    assert.equal(card?.chip, "interrupted by restart");
  });

  test("the detail line names the pending approvals, who started it, or the prompt", async () => {
    const architect = session({ role: "architect", agentId: "arch" });
    const started = session({ parentSessionId: architect.id, agentId: "rev" });
    const approving = session({ pendingApprovals: 2 });
    const prompted = session({ task: "Fix the flaky test" });
    const vm = await board([architect, started, approving, prompted]);
    const detail = (id: string) => vm.cards.find((c) => c.id === id)?.line;
    assert.deepEqual(detail(started.id), { lead: "started by", text: "software-architect" });
    assert.equal(detail(approving.id)?.lead, "2 approvals pending");
    assert.deepEqual(detail(prompted.id), { lead: "prompt", text: "“Fix the flaky test”" });
  });
});
