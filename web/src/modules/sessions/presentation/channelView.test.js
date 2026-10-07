import { assert, file, test } from "../../../shared/testing/test.js";
import { makeMessage } from "../testing/channel-fixtures.js";
import { makeSession, T0 } from "../testing/fixtures.js";
import { announceMessage, messageView, partyLabel } from "./channelView.js";

file("sessions/presentation/channelView");

const sessions = [
  makeSession({ id: "arch", role: "architect", mode: "architect", agentId: "lead" }),
  makeSession({ id: "d1", role: "delegate", parentSessionId: "arch", agentId: "go", task: "Build the server" }),
];
const ctx = {
  sessions,
  agentNames: { lead: "software-architect", go: "go-developer" },
  documentTitles: { doc1: "Plan: Server" },
  artifactTitles: { a1: { title: "Flow diagram", href: "/api/artifacts/a1/view/" } },
  projectId: "p 1",
  ticketId: "t1",
  now: new Date(T0.getTime() + 4 * 60_000),
};

test("a party reads as the architect, its agent, or a deleted session", () => {
  assert.equal(partyLabel("arch", ctx), "the architect");
  assert.equal(partyLabel("d1", ctx), "go-developer");
  assert.equal(partyLabel("0123456789abcdef", ctx), "a deleted session 01234567");
});

test("a status report reads from → to, with its kind, its status, its age and its links", () => {
  const v = messageView(makeMessage({ id: "m1", status: "ready_for_review", documentIds: ["doc1", "doc-new"], artifactIds: ["a1", "a-gone-123456789"] }), ctx);
  assert.deepEqual([v.id, v.from, v.to, v.kind, v.age], ["m1", "go-developer", "the architect", "status report", "4m"]);
  assert.deepEqual(v.badges, [{ word: "ready for review", tone: null }]);
  assert.deepEqual(v.documents, [
    { id: "doc1", title: "Plan: Server", href: "/projects/p%201/tasks/t1/documents/doc1" },
    { id: "doc-new", title: "Document", href: "/projects/p%201/tasks/t1/documents/doc-new" },
  ]);
  assert.deepEqual(v.artifacts, [
    { id: "a1", title: "Flow diagram", href: "/api/artifacts/a1/view/" },
    { id: "a-gone-123456789", title: "Artifact a-gone-1", href: "" },
  ]);
  assert.equal(v.hasLinks, true);
  assert.equal(v.undelivered, false);
});

test("badges follow the design rules: running teal, waiting-on-someone amber, done quiet", () => {
  const badges = (fields) => messageView(makeMessage(fields), ctx).badges;
  assert.deepEqual(badges({ status: "working" }), [{ word: "working", tone: "signal" }]);
  assert.deepEqual(badges({ status: "blocked" }), [{ word: "blocked", tone: "attention" }]);
  assert.deepEqual(badges({ status: "done" }), [{ word: "done", tone: null }]);
  assert.deepEqual(badges({ kind: "reply", status: "", verdict: "approved" }), [{ word: "approved", tone: null }]);
  assert.deepEqual(badges({ kind: "reply", status: "", verdict: "changes_requested" }), [{ word: "changes requested", tone: "attention" }]);
  assert.deepEqual(badges({ kind: "question", status: "" }), []);
});

test("a reply names what it answers; one not yet delivered says so", () => {
  const question = makeMessage({ id: "q", kind: "question", status: "", subject: "", body: "Which port?\nThe old one is taken." });
  const reply = makeMessage({ id: "r", kind: "reply", status: "", fromSessionId: "arch", toSessionId: "d1", inReplyTo: "q", delivered: false, deliveredAt: null });
  const v = messageView(reply, { ...ctx, messages: [question, reply] });
  assert.equal(v.replyTo, "Which port?");
  assert.equal(v.undelivered, true);
  assert.equal(messageView(makeMessage({ inReplyTo: "gone" }), { ...ctx, messages: [] }).replyTo, "an earlier message");
  assert.equal(messageView(makeMessage({ inReplyTo: "q2" }), { ...ctx, messages: [makeMessage({ id: "q2", subject: "Port" })] }).replyTo, "Port");
});

test("a message arriving is announced in words", () => {
  assert.equal(announceMessage(makeMessage({ status: "ready_for_review" }), ctx), "go-developer reported: ready for review");
  assert.equal(announceMessage(makeMessage({ kind: "question", status: "" }), ctx), "go-developer asked the architect a question");
  assert.equal(announceMessage(makeMessage({ kind: "review_request", status: "" }), ctx), "go-developer asked the architect for a review");
  assert.equal(
    announceMessage(makeMessage({ kind: "reply", status: "", verdict: "changes_requested", fromSessionId: "arch", toSessionId: "d1" }), ctx),
    "the architect replied to go-developer: changes requested",
  );
});

test("a long body is folded; a short one is not", () => {
  assert.equal(messageView(makeMessage({ body: "short" }), ctx).long, false);
  assert.equal(messageView(makeMessage({ body: Array(9).fill("line").join("\n") }), ctx).long, true);
  assert.equal(messageView(makeMessage({ body: "x".repeat(801) }), ctx).long, true);
});
