import { Codes } from "../../../shared/domain/errors.js";
import { assert, file, test } from "../../../shared/testing/test.js";
import { KINDS, OUTCOMES } from "../domain/notification.js";
import { toNotification, toPage } from "../infrastructure/dto.js";
import { at, makeNotification, N0 } from "../testing/fixtures.js";
import { foldRuns, groupsFor } from "./ledger.js";
import { bellLabel, badgeText, toRow, toStackRow } from "./view.js";

file("notifications/presentation/ledger-matrix");

// Gaps in the ledger's unit coverage, from the Spec: every kind in every state
// (open, each outcome, settled with none recorded), revision 5 against
// revision 6 wire data, the edges of folding, and what the row reads as.

const clock = (d) => `${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}`;
const day = (y, m, d, h = 12, min = 0) => new Date(y, m - 1, d, h, min, 0);

/** The words the person reads for each outcome (Spec story 7), written out here, not taken from the code. */
const WORDS = {
  approved: "Approved",
  changes_requested: "Changes requested",
  denied: "Denied",
  expired: "Expired",
  withdrawn: "Withdrawn",
  answered: "Answered",
  dismissed: "Dismissed",
  acknowledged: "Acknowledged",
  repaired: "Repaired",
  resumed: "Resumed",
  ended: "Ended",
  done: "Done",
  reopened: "Reopened",
};

/** A revision 6 notification as the server sends it, per kind (contract §1). */
const REV6 = {
  review_requested: { level: "action", actor: { kind: "agent", name: "architect-1", mode: "architect", session_id: "s-arch" }, verb: "asks you to review", object: { text: "Plan: the ledger", code: false } },
  session_waiting: { level: "action", actor: { kind: "agent", name: "go-developer", mode: "implementing", session_id: "s-dev" }, verb: "waits for your approval to run", object: { text: "go test ./internal/...", code: true } },
  task_review: { level: "action", actor: { kind: "agent", name: "go-developer", mode: "implementing", session_id: "s-dev" }, verb: "moved the task to review", object: { text: "Redesign sidebar", code: false } },
  session_failed: { level: "info", actor: { kind: "agent", name: "devops", mode: "operate", session_id: "s-ops" }, verb: "failed while running", object: { text: "make build", code: true } },
  commit_check_off: { level: "action", actor: { kind: "system", name: "harness" }, verb: "could not guard 2 sessions", object: { text: "commit checks off", code: false } },
  branch_request: { level: "action", actor: { kind: "agent", name: "devops", mode: "operate", session_id: "s-ops" }, verb: "wants to push", object: { text: "agent/x → origin/feat/y", code: true } },
  branch_finding: { level: "action", actor: { kind: "system", name: "branch watch" }, verb: "found a push with no approved request on", object: { text: "agent/x", code: true } },
  service_request: { level: "action", actor: { kind: "agent", name: "devops", mode: "operate", session_id: "s-ops" }, verb: "wants to restart", object: { text: "api on main", code: true } },
  sessions_interrupted: { level: "action", actor: { kind: "system", name: "restart" }, verb: "interrupted 2 sessions", object: { text: "sessions", code: false } },
  agent: { level: "info", actor: { kind: "agent", name: "devops", mode: "operate", session_id: "s-ops" } },
};

const wire = (kind, over = {}) => ({
  id: `n-${kind}`,
  project_id: "p1",
  project_name: "harness",
  task_id: "t1",
  task_title: "Notification",
  source: kind === "agent" ? "agent" : "system",
  kind,
  title: `Title of ${kind}`,
  body: "A body.",
  link: "/projects/p1/tasks/t1",
  created_at: N0.toISOString(),
  ...REV6[kind],
  ...over,
});

const rowOf = (kind, over) => toRow(toNotification(wire(kind, over)), N0);

test("every kind × open: Needs you for an open cause, the verb and object as the what, who with the mode", () => {
  for (const kind of KINDS) {
    const row = rowOf(kind);
    const w = REV6[kind];
    assert.equal(row.kind, kind);
    assert.equal(row.whoName, w.actor.name, kind);
    assert.equal(row.whoKind, w.actor.kind, kind);
    assert.equal(row.whoInitial, w.actor.name[0], kind);
    assert.equal(row.whoMode, w.actor.mode ?? "", kind);
    assert.equal(row.whoIsSystem, w.actor.kind === "system", kind);
    assert.equal(row.whoSlot === "", w.actor.kind !== "agent", `${kind}: only an agent has a colour slot`);
    if (kind === "agent") {
      assert.deepEqual([row.hasVerb, row.hasObject], [false, false], "an agent notification has no verb");
      assert.ok(row.label.includes(`${w.actor.name} Title of agent.`), "its own title is the what");
    } else {
      assert.deepEqual([row.hasVerb, row.verb, row.objectText, row.objectIsCode], [true, w.verb, w.object.text, w.object.code], kind);
      assert.ok(row.label.includes(`${w.actor.name} ${w.verb} ${w.object.text}.`), `${kind}: ${row.label}`);
    }
    const open = w.level === "action";
    if (open) assert.deepEqual([row.statusText, row.statusTone, row.levelLabel, row.settled, row.state], ["Needs you", "attention", "Needs you", false, "unread"], kind);
    else assert.equal(row.settled, false, kind);
    assert.equal(row.canAcknowledge, kind === "branch_finding", kind);
    assert.equal(row.canResumeAll, kind === "sessions_interrupted", kind);
  }
});

test("every kind × every outcome: the outcome word and its time, quiet row, no actions left", () => {
  const resolved = at(95);
  for (const kind of KINDS) {
    for (const outcome of OUTCOMES) {
      const row = rowOf(kind, { resolved_at: resolved.toISOString(), outcome });
      assert.equal(row.statusText, `${WORDS[outcome]} ${clock(resolved)}`, `${kind} ${outcome}`);
      assert.deepEqual([row.settled, row.state, row.canAcknowledge, row.canResumeAll, row.hasStatus], [true, "settled", false, false, true], `${kind} ${outcome}`);
      assert.equal(row.levelLabel, "Settled");
      assert.equal(row.label.includes("Needs you"), false, `${kind} ${outcome}: a settled row does not claim to need the person`);
    }
  }
});

test("every kind settled with no outcome recorded (a row from before revision 6) reads as Settled", () => {
  for (const kind of KINDS) {
    const row = rowOf(kind, { resolved_at: at(5).toISOString() });
    assert.deepEqual([row.statusText, row.statusTone, row.state], ["Settled", "quiet", "settled"], kind);
  }
});

test("an outcome is words, never colour alone: every tone has a non-empty text", () => {
  for (const outcome of OUTCOMES) {
    const row = rowOf("branch_request", { resolved_at: at(1).toISOString(), outcome });
    assert.ok(row.statusText.length > 0 && /^[A-Z][a-z]/.test(row.statusText), outcome);
    assert.ok(["attention", "danger", "success", "quiet"].includes(row.statusTone), outcome);
  }
});

test("the full date carries weekday, day, month, year and the clock, in the format the contract shows", () => {
  const row = rowOf("branch_request");
  assert.ok(row.fullDate.endsWith(clock(N0)), row.fullDate);
  // "Tue 17 Nov 2026, 15:40": the weekday has no comma (dayLabel builds it the same way).
  assert.ok(/^[A-Z][a-z]{2} \d{1,2} [A-Z][a-z]{2} \d{4}, \d{2}:\d{2}$/.test(row.fullDate), `fullDate is ${JSON.stringify(row.fullDate)}`);
});

// ── revision 5 and revision 6 ─────────────────────────────────────────────

test("revision 5 data (no actor, verb, object, outcome) reads for every kind: title as the what, the harness as the who", () => {
  for (const kind of KINDS) {
    const v5 = { id: `o-${kind}`, project_id: "p1", project_name: "harness", source: "system", kind, level: "action", title: `Old ${kind}`, link: "/projects/p1", created_at: N0.toISOString() };
    const row = toRow(toNotification(v5), N0);
    assert.deepEqual([row.whoName, row.whoMode, row.whoIsSystem, row.whoSlot, row.hasVerb, row.hasObject, row.hasRepository], ["harness", "", true, "", false, false, false], kind);
    assert.ok(row.label.includes(`harness Old ${kind}.`), row.label);
    assert.equal(row.statusText, kind === "session_failed" ? "Failed" : "Needs you", kind);
  }
  const settled = toRow(toNotification({ id: "o", project_id: "p1", source: "system", kind: "task_review", level: "action", title: "Old", link: "/p", created_at: N0.toISOString(), resolved_at: at(3).toISOString() }), N0);
  assert.equal(settled.statusText, "Settled", "a revision 5 row settled earlier has no outcome");
});

test("a page can mix revision 5 and revision 6 rows; a missing open_count is null, and a bad one is refused", () => {
  const body = {
    notifications: [wire("branch_request", { id: "new" }), { id: "old", source: "system", kind: "session_waiting", level: "action", title: "Old wait", link: "/p", created_at: at(-5).toISOString() }],
    unread_count: 2,
  };
  const page = toPage(body);
  assert.deepEqual([page.openCount, page.notifications.map((n) => n.actor.name)], [null, ["devops", "harness"]]);
  assert.equal(toPage({ ...body, open_count: 1 }).openCount, 1);
  assert.equal(toPage({ ...body, open_count: 0 }).openCount, 0, "zero is a count, not an absence");
  assert.throws(() => toPage({ ...body, open_count: -1 }), Codes.BAD_RESPONSE);
  assert.throws(() => toPage({ ...body, open_count: "3" }), Codes.BAD_RESPONSE);
});

test("what the client does not know degrades one row, not the list: unknown kind, outcome, actor kind", () => {
  const unknownKind = rowOf("branch_request", { kind: "teleport_request", verb: undefined, object: undefined, title: "Teleport asked" });
  assert.deepEqual([unknownKind.kind, unknownKind.hasVerb, unknownKind.label.includes("Teleport asked")], ["agent", false, true]);
  const unknownOutcome = rowOf("branch_request", { resolved_at: at(2).toISOString(), outcome: "teleported" });
  assert.equal(unknownOutcome.statusText, "Settled");
  const unknownActor = rowOf("branch_request", { actor: { kind: "bot", name: "cron" } });
  assert.deepEqual([unknownActor.whoName, unknownActor.whoKind, unknownActor.whoSlot], ["cron", "system", ""]);
  const nameless = rowOf("branch_request", { actor: { kind: "agent", name: "" } });
  assert.equal(nameless.whoName, "harness");
});

test("the person is a who of their own; a verb without an object, or an empty object, reads as the verb alone", () => {
  const you = rowOf("task_review", { actor: { kind: "person", name: "you" } });
  assert.deepEqual([you.whoName, you.whoKind, you.whoSlot, you.whoIsSystem], ["you", "person", "", false]);
  const bare = rowOf("task_review", { object: undefined });
  assert.deepEqual([bare.hasObject, bare.objectText, bare.label.includes("moved the task to review.")], [false, "", true]);
  const empty = rowOf("task_review", { object: { text: "", code: true } });
  assert.deepEqual([empty.hasObject, empty.objectIsCode], [false, false]);
});

test("where reads project › task · repository, leaving out what is absent", () => {
  const full = rowOf("branch_request", { repository_name: "harness-api" });
  assert.ok(full.label.includes("harness › Notification · harness-api."), full.label);
  const noTask = rowOf("branch_request", { task_id: undefined, task_title: undefined, repository_name: "harness-api" });
  assert.ok(noTask.label.includes(" harness · harness-api."), noTask.label);
  assert.equal(noTask.hasTask, false);
  const bare = rowOf("agent", { task_id: undefined, task_title: undefined });
  assert.ok(bare.label.includes(" harness."), bare.label);
});

// ── the badge counts what needs the person ────────────────────────────────

test("the badge and the bell's name: open and unread are two numbers; the badge is the open one", () => {
  assert.deepEqual([badgeText(0), badgeText(1), badgeText(99), badgeText(100)], ["", "1", "99", "99+"]);
  assert.equal(bellLabel(0, 7), "Notifications, 7 unread", "unread alone is named, though it puts no count on the badge");
  assert.equal(bellLabel(1, 0), "Notifications, 1 need you");
  assert.equal(bellLabel(1, 4), "Notifications, 1 need you, 4 unread");
});

// ── folding: edges ────────────────────────────────────────────────────────

const agentBy = (sessionId, name = "devops") => ({ kind: "agent", name, mode: "operate", sessionId });
const settledPush = (id, created, over = {}) =>
  makeNotification({ id, kind: "branch_request", createdAt: created, resolvedAt: new Date(created.getTime() + 60_000), outcome: "approved", actor: agentBy("s1"), verb: "wants to push", object: { text: "agent/x → origin/feat/y", code: true }, ...over });
const NOW = day(2026, 11, 17, 15);
const runsOf = (items, tab = "all") =>
  groupsFor(items, tab, NOW)
    .filter((g) => g.key !== "needs")
    .map((g) => foldRuns(g.items, g.breakBefore).map((r) => r.items.map((n) => n.id)));

test("the day boundary: two settled items of one session and kind across local midnight do not fold", () => {
  const items = [settledPush("late", day(2026, 11, 17, 0, 1)), settledPush("early", day(2026, 11, 16, 23, 59))];
  assert.deepEqual(runsOf(items), [[["late"]], [["early"]]]);
  const sameDay = [settledPush("b", day(2026, 11, 17, 0, 2)), settledPush("a", day(2026, 11, 17, 0, 1))];
  assert.deepEqual(runsOf(sameDay), [[["b", "a"]]]);
});

test("a run is consecutive: another session or kind between two items splits them, even when they match around it", () => {
  const items = [settledPush("a", at(50)), settledPush("x", at(40), { actor: agentBy("s2") }), settledPush("b", at(30)), settledPush("k", at(20), { kind: "service_request" }), settledPush("c", at(10))];
  assert.deepEqual(runsOf(items).flat(), [["a"], ["x"], ["b"], ["k"], ["c"]]);
});

test("an open cause between two settled items of one session breaks the run; the cause is pinned in Needs you, not folded", () => {
  const open = makeNotification({ id: "o", kind: "branch_request", createdAt: at(40), actor: agentBy("s1"), verb: "wants to push", object: { text: "agent/z", code: true } });
  const items = [settledPush("a", at(50)), open, settledPush("b", at(30)), settledPush("c", at(20))];
  const groups = groupsFor(items, "all", NOW);
  assert.deepEqual(groups[0].items.map((n) => n.id), ["o"]);
  assert.deepEqual(runsOf(items), [[["a"], ["b", "c"]]]);
  // And two open causes of the session, with settled ones around, never fold with each other.
  const open2 = makeNotification({ id: "o2", kind: "branch_request", createdAt: at(45), actor: agentBy("s1") });
  assert.deepEqual(groupsFor([open2, open], "all", NOW).map((g) => g.items.length), [2]);
});

test("an unread information item that is not settled never joins a stack", () => {
  const info = (id, m) => makeNotification({ id, kind: "agent", level: "info", createdAt: at(m), actor: agentBy("s1") });
  assert.deepEqual(runsOf([info("a", 30), info("b", 20), info("c", 10)]).flat(), [["a"], ["b"], ["c"]]);
});

test("no session id: items from the harness, the system or the person never fold, even all alike", () => {
  const sys = (id, m, actor) => settledPush(id, at(m), { actor, kind: "sessions_interrupted" });
  for (const actor of [{ kind: "system", name: "restart", mode: "", sessionId: "" }, { kind: "person", name: "you", mode: "", sessionId: "" }, { kind: "system", name: "harness", mode: "", sessionId: "" }]) {
    assert.deepEqual(runsOf([sys("a", 30, actor), sys("b", 20, actor), sys("c", 10, actor)]).flat(), [["a"], ["b"], ["c"]], actor.name);
  }
});

test("a stack of two says 2 times and counts its outcomes; legacy settled counts as settled", () => {
  const two = [settledPush("a", at(30), { outcome: "approved" }), settledPush("b", at(20), { outcome: "" })];
  const row = toStackRow(two, NOW);
  assert.deepEqual([row.count, row.verb, row.statusText, row.state, row.unread], [2, "asked to push 2 times", "1 approved · 1 settled", "settled", false]);
  assert.ok(row.label.endsWith("Expand to see them."));
  assert.ok(row.label.includes("devops asked to push 2 times agent/x → origin/feat/y."), row.label);
});

test("a stack whose items name different objects names none; one that shares it keeps it as code", () => {
  const mixed = [settledPush("a", at(30)), settledPush("b", at(20), { object: { text: "agent/other → origin/feat/z", code: true } })];
  const row = toStackRow(mixed, NOW);
  assert.deepEqual([row.hasObject, row.objectText], [false, ""]);
  const same = toStackRow([settledPush("a", at(30)), settledPush("b", at(20))], NOW);
  assert.deepEqual([same.hasObject, same.objectIsCode], [true, true]);
});

test("a stack's key is its oldest item, so a newer arrival does not move an opened stack", () => {
  const before = foldRuns([settledPush("b", at(30)), settledPush("a", at(20))]);
  const after = foldRuns([settledPush("c", at(40)), settledPush("b", at(30)), settledPush("a", at(20))]);
  assert.deepEqual([before[0].key, after[0].key, after[0].items.length], ["a", "a", 3]);
});

test("a stack of mixed outcomes counts each word, most frequent first, ties in the outcome words' own order", () => {
  const items = ["denied", "approved", "denied", "expired", "approved", "denied"].map((o, i) => settledPush(`n${i}`, at(60 - i), { outcome: o }));
  assert.equal(toStackRow(items, NOW).statusText, "3 denied · 2 approved · 1 expired");
  const tie = ["expired", "approved"].map((o, i) => settledPush(`t${i}`, at(60 - i), { outcome: o }));
  assert.equal(toStackRow(tie, NOW).statusText, "1 approved · 1 expired");
});

// ── days ──────────────────────────────────────────────────────────────────

test("a day label crosses a month and a year: Yesterday on 31 Dec when it is 1 Jan, then the date with its year", () => {
  const now = day(2027, 1, 1, 12);
  const one = (created) => groupsFor([makeNotification({ id: "a", level: "info", createdAt: created })], "all", now)[0].title;
  assert.equal(one(day(2026, 12, 31, 23, 30)), "Yesterday");
  assert.equal(one(day(2027, 1, 1, 0, 5)), "Today");
  assert.equal(one(day(2026, 12, 30, 12)), "Wed 30 Dec 2026");
});

test("Needs you leads even when every open cause is older than the settled ones, and an empty list has no groups", () => {
  const items = [settledPush("a", day(2026, 11, 17, 14)), makeNotification({ id: "o", createdAt: day(2026, 11, 3, 9) })];
  assert.deepEqual(groupsFor(items, "all", NOW).map((g) => g.key), ["needs", "day:2026-11-17"]);
  assert.deepEqual(groupsFor([], "all", NOW), []);
  assert.deepEqual(groupsFor([], "needs", NOW), []);
});
