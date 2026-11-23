import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { memoryBoardSource } from "../../infrastructure/memory-board-source";
import type { ReviewRequest, TaskMessage } from "../../domain/message";
import type { BoardSession } from "../../domain/session";
import { AGENT_NAMES, T0, message, review, session } from "../../testing/fixtures";
import { BoardPresenter } from "./board-presenter";

const NOW = new Date(T0 + 3_600_000);
const ago = (minutes: number): Date => new Date(NOW.getTime() - minutes * 60_000);

async function board(sessions: BoardSession[], messages: TaskMessage[] = [], reviews: ReviewRequest[] = []) {
  const source = memoryBoardSource(sessions);
  source.setMessages(messages);
  source.setReviews(reviews);
  const presenter = new BoardPresenter({ source, ticketId: "t1", projectId: "p1", agentNames: AGENT_NAMES, taskTitle: "Ship it", now: () => NOW });
  await presenter.start();
  return presenter.viewModel;
}

function team() {
  const architect = session({ role: "architect", agentId: "arch" });
  const reviewer = session({ role: "delegate", mode: "review", agentId: "rev", parentSessionId: architect.id });
  return { architect, reviewer };
}

const edge = (vm: Awaited<ReturnType<typeof board>>, to: string) => vm.edges.find((e) => e.to === to)!;

describe("thread badges", () => {
  test("count the messages between the two sessions in both directions and name the latest", async () => {
    const { architect, reviewer } = team();
    const messages = [
      message(architect.id, reviewer.id, { id: "q", kind: "question", createdAt: ago(40) }),
      message(reviewer.id, architect.id, { kind: "reply", inReplyTo: "q", createdAt: ago(30) }),
      message(reviewer.id, architect.id, { kind: "status_report", createdAt: ago(11) }),
    ];
    const badge = edge(await board([architect, reviewer], messages), reviewer.id).badge;
    assert.equal(badge?.count, 3);
    assert.equal(badge?.text, "report 11m");
  });

  test("an edge with no messages has no badge", async () => {
    const { architect, reviewer } = team();
    assert.equal(edge(await board([architect, reviewer]), reviewer.id).badge, null);
  });

  test("messages of another pair do not count", async () => {
    const { architect, reviewer } = team();
    const other = session({ role: "delegate", parentSessionId: architect.id });
    const vm = await board([architect, reviewer, other], [message(architect.id, other.id)]);
    assert.equal(edge(vm, reviewer.id).badge, null);
    assert.equal(edge(vm, other.id).badge?.count, 1);
  });

  test("a question or review request with no reply is pending: signal tone and a wait", async () => {
    const { architect, reviewer } = team();
    const q = message(reviewer.id, architect.id, { kind: "question", createdAt: ago(4) });
    const vm = await board([architect, reviewer], [q]);
    assert.equal(edge(vm, reviewer.id).tone, "signal");
    assert.equal(edge(vm, reviewer.id).badge?.text, "question · waits 4m");
    assert.equal(edge(vm, reviewer.id).badge?.tone, "signal");
  });

  test("a later message that replies to it clears the pending state", async () => {
    const { architect, reviewer } = team();
    const q = message(reviewer.id, architect.id, { kind: "question", createdAt: ago(10) });
    const answer = message(architect.id, reviewer.id, { kind: "reply", inReplyTo: q.id, createdAt: ago(3) });
    const vm = await board([architect, reviewer], [q, answer]);
    assert.equal(edge(vm, reviewer.id).tone, "started");
    assert.equal(edge(vm, reviewer.id).badge?.text, "reply 3m");
  });

  test("the badge's accessible name is the full sentence with both agents and the age", async () => {
    const { architect, reviewer } = team();
    const vm = await board([architect, reviewer], [message(reviewer.id, architect.id, { kind: "status_report", createdAt: ago(20) }), message(architect.id, reviewer.id, { kind: "reply", createdAt: ago(11) })]);
    assert.equal(edge(vm, reviewer.id).badge?.name, "2 messages between software-architect and code-reviewer, latest reply 11 minutes ago");
    const waiting = await board([architect, reviewer], [message(reviewer.id, architect.id, { kind: "question", createdAt: ago(1) })]);
    assert.equal(edge(waiting, reviewer.id).badge?.name, "1 message between software-architect and code-reviewer, question waits 1 minute");
  });

  test("a card names what waits on the architect", async () => {
    const { architect, reviewer } = team();
    const vm = await board([architect, reviewer], [message(reviewer.id, architect.id, { kind: "question", createdAt: ago(2) })]);
    assert.equal(vm.cards.find((c) => c.id === reviewer.id)?.name, "code-reviewer, review, running, question waiting on the architect");
    assert.equal(vm.cards.find((c) => c.id === architect.id)?.name, "software-architect, architect, running");
  });

  test("a delegate's detail line is its latest report", async () => {
    const { architect, reviewer } = team();
    const vm = await board([architect, reviewer], [message(reviewer.id, architect.id, { kind: "status_report", status: "ready_for_review", body: "All  green", createdAt: ago(5) })]);
    assert.deepEqual(vm.cards.find((c) => c.id === reviewer.id)?.line, { lead: "ready for review", text: "“All green”" });
    const none = await board([architect, reviewer]);
    assert.equal(none.cards.find((c) => c.id === reviewer.id)?.line?.lead, "no report yet");
  });
});

describe("the You card", () => {
  test("its edge to the architect is the grey started edge until a review request waits on the person", async () => {
    const { architect, reviewer } = team();
    const quiet = await board([architect, reviewer]);
    assert.equal(edge(quiet, architect.id).tone, "started");
    assert.equal(edge(quiet, architect.id).badge, null);
    assert.equal(quiet.cards[0]?.attention, false);
  });

  test("a pending review request turns that edge amber with its badge and the You card to attention", async () => {
    const { architect, reviewer } = team();
    const vm = await board([architect, reviewer], [], [review(architect.id, { createdAt: ago(20) })]);
    const e = edge(vm, architect.id);
    assert.equal(e.tone, "amber");
    assert.equal(e.badge?.text, "review request · waits 20m");
    assert.equal(e.badge?.count, 1);
    assert.equal(e.badge?.tone, "attention");
    assert.equal(e.badge?.name, "1 review request waits on you, 20 minutes");
    assert.equal(vm.cards[0]?.attention, true);
    assert.equal(vm.cards[0]?.name, "You, 1 review request waiting on you");
  });

  test("an answered review request leaves the edge grey", async () => {
    const { architect } = team();
    const vm = await board([architect], [], [review(architect.id, { state: "approved" })]);
    assert.equal(edge(vm, architect.id).tone, "started");
  });

  test("the badge counts several waiting requests and waits from the oldest", async () => {
    const { architect } = team();
    const vm = await board([architect], [], [review(architect.id, { createdAt: ago(5) }), review(architect.id, { createdAt: ago(30) })]);
    assert.equal(edge(vm, architect.id).badge?.count, 2);
    assert.equal(edge(vm, architect.id).badge?.text, "review request · waits 30m");
  });
});

describe("scale", () => {
  test("500 messages across 20 sessions derive well within a second", async () => {
    const architect = session({ role: "architect" });
    const delegates = Array.from({ length: 19 }, () => session({ role: "delegate", mode: "implement", parentSessionId: architect.id }));
    const messages = Array.from({ length: 500 }, (_, i) => message(delegates[i % 19]!.id, architect.id, { kind: i % 7 === 0 ? "question" : "status_report", createdAt: ago(500 - i) }));
    const started = performance.now();
    const vm = await board([architect, ...delegates], messages);
    assert.ok(performance.now() - started < 1000);
    assert.equal(vm.edges.length, 20);
  });
});
