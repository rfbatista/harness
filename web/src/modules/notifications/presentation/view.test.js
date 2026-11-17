import { assert, file, test } from "../../../shared/testing/test.js";
import { makeNotification, N0 } from "../testing/fixtures.js";
import { Codes, StructuredError } from "../../../shared/domain/errors.js";
import { FeedStatus } from "../../../shared/domain/feed.js";
import { agentSlot } from "../../architecture/domain/layers.js";
import { badgeText, bellLabel, inboxError, streamNotice, toRow } from "./view.js";

file("notifications/presentation/view");

const restart = (fields = {}) =>
  makeNotification({
    kind: "sessions_interrupted",
    link: "/notifications",
    interruptedSessions: [
      { sessionId: "s1", projectId: "p1", taskId: "t1", taskTitle: "Ship", agentName: "go-developer", link: "/projects/p1/tasks/t1?session=s1", state: "interrupted", reason: "" },
      { sessionId: "s2", projectId: "p1", taskId: "", taskTitle: "", agentName: "devops", link: "/projects/p1?session=s2", state: "failed", reason: "the worktree is gone" },
    ],
    ...fields,
  });

test("a sessions_interrupted item lists each session with its task, agent, state and the reason a resume failed", () => {
  const row = toRow(restart(), N0);
  assert.deepEqual(row.sessions.map((s) => [s.key, s.label, s.href, s.state, s.reason, s.hasReason]), [
    ["s1", "Ship · go-developer", "/projects/p1/tasks/t1?session=s1", "interrupted", "", false],
    ["s2", "No task · devops", "/projects/p1?session=s2", "failed", "the worktree is gone", true],
  ]);
  assert.deepEqual([row.hasSessions, row.canResumeAll], [true, true]);
});

test("Resume all interrupted is offered while the item is not settled, and other kinds have no list", () => {
  assert.equal(toRow(restart({ resolvedAt: N0 }), N0).canResumeAll, false);
  const plain = toRow(makeNotification(), N0);
  assert.deepEqual([plain.sessions, plain.hasSessions, plain.canResumeAll], [[], false, false]);
});

test("an inbox error says what happened in plain words, never a code or a developer message", () => {
  const network = inboxError(new StructuredError(Codes.NETWORK, "Failed to fetch"));
  assert.equal(network.message, "The notifications could not be loaded. The harness server did not answer.");
  const disagree = inboxError(new StructuredError(Codes.BAD_RESPONSE, "Unexpected notification: n1 has unknown kind."));
  assert.equal(disagree.message, "The notifications could not be read. Reload; if it stays, check the harness server log.");
  const other = inboxError(new Error("boom: sql: no rows"));
  assert.equal(other.message, "The notifications could not be loaded.");
  for (const e of [network, disagree, other]) assert.equal(/BAD_RESPONSE|NETWORK|Unexpected|boom|Failed to fetch/.test(e.message), false);
});

test("a stream that dropped says so, and reads nothing while it is live", () => {
  assert.equal(streamNotice(FeedStatus.PAUSED), "Live updates are paused. Reconnecting; Reload shows what is there now.");
  for (const s of [FeedStatus.LIVE, FeedStatus.CONNECTING, FeedStatus.RESYNCED]) assert.equal(streamNotice(s), "");
});

// ── the ledger row (ADR 0095) ─────────────────────────────────────────────

const clock = (d) => `${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}`;
const agentPush = (fields = {}) =>
  makeNotification({
    source: "system",
    kind: "branch_request",
    title: "devops wants to push agent/x to origin/feat/y",
    body: "CI is green.",
    repositoryName: "harness-api",
    actor: { kind: "agent", name: "devops", mode: "operate", sessionId: "s9" },
    verb: "wants to push",
    object: { text: "agent/x → origin/feat/y", code: true },
    ...fields,
  });

test("who: an agent shows its initial, name and mode; the system shows its source and no mode", () => {
  const row = toRow(agentPush(), N0);
  assert.deepEqual([row.whoName, row.whoMode, row.whoInitial, row.whoIsSystem], ["devops", "operate", "d", false]);
  assert.equal(row.whoSlot, String(agentSlot("devops")));
  const watch = toRow(makeNotification({ kind: "branch_finding", actor: { kind: "system", name: "branch watch", mode: "", sessionId: "" } }), N0);
  assert.deepEqual([watch.whoName, watch.whoMode, watch.whoIsSystem], ["branch watch", "", true]);
});

test("the same agent has the same colour in every row", () => {
  assert.equal(toRow(agentPush({ id: "a" }), N0).whoSlot, toRow(agentPush({ id: "b", kind: "service_request" }), N0).whoSlot);
});

test("what: the verb and the object, code objects flagged, then project, task and repository", () => {
  const row = toRow(agentPush(), N0);
  assert.deepEqual([row.hasVerb, row.verb, row.objectText, row.objectIsCode], [true, "wants to push", "agent/x → origin/feat/y", true]);
  assert.deepEqual([row.project, row.task, row.repository, row.hasRepository], ["harness", "Notification", "harness-api", true]);
  const text = toRow(agentPush({ verb: "asks you to review", object: { text: "the tickets", code: false } }), N0);
  assert.equal(text.objectIsCode, false);
});

test("an agent notification shows its own title as the what", () => {
  const row = toRow(makeNotification({ kind: "agent", level: "info", title: "Release notes drafted", actor: { kind: "agent", name: "writer", mode: "session", sessionId: "s2" } }), N0);
  assert.deepEqual([row.hasVerb, row.title, row.whoName], [false, "Release notes drafted", "writer"]);
});

test("revision 5 data reads sensibly: the title is the what and the harness is the who", () => {
  const row = toRow(makeNotification({ title: "A session waits for approval" }), N0);
  assert.deepEqual([row.whoName, row.whoMode, row.whoIsSystem, row.hasVerb, row.title, row.hasRepository], ["harness", "", true, false, "A session waits for approval", false]);
});

test("when: the clock time and age, the full date for the tooltip, and where it stands", () => {
  const now = new Date(N0.getTime() + 17 * 60_000);
  const open = toRow(agentPush(), now);
  assert.deepEqual([open.clock, open.age, open.statusText, open.statusTone], [clock(N0), "17m", "Needs you", "attention"]);
  assert.equal(open.fullDate.includes("2026"), true);
  const approved = toRow(agentPush({ resolvedAt: new Date(N0.getTime() + 60_000), outcome: "approved" }), now);
  assert.deepEqual([approved.statusText, approved.statusTone, approved.state], [`Approved ${clock(new Date(N0.getTime() + 60_000))}`, "success", "settled"]);
  const denied = toRow(agentPush({ resolvedAt: N0, outcome: "denied" }), now);
  assert.deepEqual([denied.statusText.startsWith("Denied "), denied.statusTone], [true, "danger"]);
  const ack = toRow(agentPush({ kind: "branch_finding", resolvedAt: N0, outcome: "acknowledged" }), now);
  assert.equal(ack.statusText.startsWith("Acknowledged "), true);
  const old = toRow(agentPush({ resolvedAt: N0 }), now);
  assert.deepEqual([old.statusText, old.statusTone], ["Settled", "quiet"]);
});

test("an information item that is not settled has no standing line, and a failed session says Failed", () => {
  const info = toRow(makeNotification({ kind: "agent", level: "info" }), N0);
  assert.deepEqual([info.statusText, info.hasStatus], ["", false]);
  const failed = toRow(makeNotification({ kind: "session_failed", level: "info" }), N0);
  assert.deepEqual([failed.statusText, failed.statusTone], ["Failed", "danger"]);
});

test("the link's accessible name reads who, what, where and when in order", () => {
  const now = new Date(N0.getTime() + 17 * 60_000);
  const row = toRow(agentPush(), now);
  assert.equal(row.label, `Unread. Needs you. devops wants to push agent/x → origin/feat/y. harness › Notification · harness-api. ${row.fullDate}, 17m ago.`);
  const read = toRow(agentPush({ readAt: N0, kind: "agent", verb: "", object: null, title: "Built" }), now);
  assert.equal(read.label.startsWith("Needs you. devops Built."), true);
});

test("only an open branch finding offers Acknowledge", () => {
  const finding = (fields = {}) => makeNotification({ kind: "branch_finding", level: "action", actor: { kind: "system", name: "branch watch", mode: "", sessionId: "" }, verb: "found a push with no approved request on", object: { text: "agent/x", code: true }, ...fields });
  const row = toRow(finding(), N0);
  assert.deepEqual([row.canAcknowledge, row.ackLabel], [true, "Acknowledge: branch watch found a push with no approved request on agent/x"]);
  assert.equal(toRow(finding({ resolvedAt: N0, outcome: "acknowledged" }), N0).canAcknowledge, false);
  assert.equal(toRow(agentPush(), N0).canAcknowledge, false);
});

test("a kind's glyph and the row's kind reach the markup; the old fields stay", () => {
  const row = toRow(agentPush(), N0);
  assert.deepEqual([row.kind, row.href, row.unread, row.canResumeAll], ["branch_request", "/projects/p1/tasks/t1", true, false]);
});

test("the bell's name says what its count counts", () => {
  assert.equal(bellLabel(3, true), "Notifications, 3 need you");
  assert.equal(bellLabel(3, true, 2), "Notifications, 3 need you, 2 unread");
  assert.equal(bellLabel(3, false), "Notifications, 3 unread");
  assert.equal(bellLabel(3), "Notifications, 3 unread");
  assert.equal(bellLabel(0, true), "Notifications");
  assert.equal(badgeText(120), "99+");
});

// ── a folded stack's summary (ADR 0097) ───────────────────────────────────

import { toStackRow } from "./view.js";

const settledPush = (id, m, outcome, fields = {}) =>
  agentPush({ id, createdAt: new Date(N0.getTime() + m * 60_000), resolvedAt: new Date(N0.getTime() + m * 60_000 + 30_000), outcome, ...fields });

test("a stack counts its outcomes in the agent's voice, with the newest time and where it happened", () => {
  const items = [settledPush("c", 30, "approved"), settledPush("b", 20, "denied"), settledPush("a", 10, "approved")];
  const row = toStackRow(items, new Date(N0.getTime() + 60 * 60_000));
  assert.deepEqual([row.count, row.whoName, row.whoMode, row.verb, row.statusText], [3, "devops", "operate", "asked to push 3 times", "2 approved · 1 denied"]);
  assert.deepEqual([row.objectText, row.objectIsCode, row.project, row.task, row.repository], ["agent/x → origin/feat/y", true, "harness", "Notification", "harness-api"]);
  assert.equal(row.clock, clock(new Date(N0.getTime() + 30 * 60_000)));
  assert.equal(row.label, `2 approved · 1 denied. devops asked to push 3 times agent/x → origin/feat/y. harness › Notification · harness-api. ${row.fullDate}, 30m ago. Expand to see them.`);
});

test("the stack names the object only when every item shares it, and settled with no outcome counts as settled", () => {
  const mixed = toStackRow([settledPush("b", 20, "approved", { object: { text: "agent/y", code: true } }), settledPush("a", 10, "")], N0);
  assert.deepEqual([mixed.hasObject, mixed.objectText, mixed.statusText], [false, "", "1 approved · 1 settled"]);
});

test("verbs read in the past: wants to, asks you to, waits for", () => {
  const verb = (v) => toStackRow([settledPush("b", 20, "approved", { verb: v }), settledPush("a", 10, "approved", { verb: v })], N0).verb;
  assert.deepEqual([verb("asks you to review"), verb("waits for your approval to run"), verb("wants to restart")], ["asked you to review 2 times", "waited for your approval to run 2 times", "asked to restart 2 times"]);
});
