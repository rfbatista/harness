import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { agentSlot } from "../../domain/agent-slot";
import { HARNESS_ACTOR } from "../../domain/notification";
import { notification } from "../../testing/fixtures";
import { toRow } from "./row";

const NOW = new Date(2026, 10, 21, 15, 40);
const at = (minutesAgo: number) => new Date(NOW.getTime() - minutesAgo * 60_000);
const hhmm = (d: Date) => `${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}`;
const agent = { kind: "agent", name: "devops", mode: "operate", sessionId: "s9" } as const;

describe("a ledger row", () => {
  test("names who: the agent, its mode and its colour slot, as the session row shows it", () => {
    const row = toRow(notification({ actor: agent }), NOW);
    assert.deepEqual([row.whoName, row.whoMode, row.whoInitial, row.whoSlot, row.whoKind], ["devops", "operate", "d", String(agentSlot("devops")), "agent"]);
  });

  test("a notification without an actor reads as the harness, with no mode and no colour", () => {
    const row = toRow(notification(), NOW);
    assert.deepEqual([row.whoName, row.whoKind, row.whoMode, row.whoSlot, row.whoInitial], [HARNESS_ACTOR.name, "system", "", "", "h"]);
  });

  test("the same agent name always has the same colour slot, in 0..7", () => {
    for (const name of ["devops", "go-developer", "software-architect", "日本"]) assert.equal(agentSlot(name), agentSlot(name));
    for (const name of ["a", "b", "devops", "go-developer"]) assert.ok(agentSlot(name) >= 0 && agentSlot(name) < 8);
  });

  test("reads as a verb and its object, or as its title when it has no verb", () => {
    const withVerb = toRow(notification({ actor: agent, verb: "wants to push", object: { text: "agent/x → origin/feat/y", code: true }, title: "devops wants to push" }), NOW);
    assert.deepEqual([withVerb.hasVerb, withVerb.verb, withVerb.hasObject, withVerb.objectText, withVerb.objectIsCode], [true, "wants to push", true, "agent/x → origin/feat/y", true]);
    const plain = toRow(notification({ title: "A note" }), NOW);
    assert.deepEqual([plain.hasVerb, plain.title, plain.hasObject], [false, "A note", false]);
  });

  test("says where: project, task and repository", () => {
    const row = toRow(notification({ projectName: "harness", taskTitle: "Fix malloc", repositoryName: "harness-api" }), NOW);
    assert.deepEqual([row.project, row.task, row.repository], ["harness", "Fix malloc", "harness-api"]);
  });

  test("says where it stands: needs you, failed, or how it was settled and when", () => {
    assert.deepEqual([toRow(notification(), NOW).statusText, toRow(notification(), NOW).statusTone], ["Needs you", "attention"]);
    assert.deepEqual([toRow(notification({ kind: "session_failed" }), NOW).statusText, toRow(notification({ kind: "session_failed" }), NOW).statusTone], ["Failed", "danger"]);
    assert.equal(toRow(notification({ level: "info" }), NOW).statusText, "");
    const resolvedAt = at(30);
    const approved = toRow(notification({ resolvedAt, outcome: "approved" }), NOW);
    assert.deepEqual([approved.statusText, approved.statusTone, approved.state], [`Approved ${hhmm(resolvedAt)}`, "success", "settled"]);
    assert.equal(toRow(notification({ resolvedAt, outcome: "denied" }), NOW).statusTone, "danger");
    assert.equal(toRow(notification({ resolvedAt, outcome: "changes_requested" }), NOW).statusText, `Changes requested ${hhmm(resolvedAt)}`);
    assert.deepEqual([toRow(notification({ resolvedAt }), NOW).statusText, toRow(notification({ resolvedAt }), NOW).statusTone], ["Settled", "quiet"]);
  });

  test("every outcome reads as words", () => {
    const outcomes = ["approved", "changes_requested", "denied", "expired", "withdrawn", "answered", "dismissed", "acknowledged", "repaired", "resumed", "ended", "done", "reopened"] as const;
    for (const outcome of outcomes) assert.match(toRow(notification({ resolvedAt: at(5), outcome }), NOW).statusText, /^[A-Z][a-z]+( [a-z]+)* \d\d:\d\d$/);
  });

  test("an open cause is not unread because it is open, and a read row stays read", () => {
    const row = toRow(notification({ readAt: at(1) }), NOW);
    assert.deepEqual([row.unread, row.state, row.statusText], [false, "read", "Needs you"]);
  });

  test("the accessible name tells unread, needs you, who and what, where, and when", () => {
    const n = notification({ actor: agent, verb: "wants to push", object: { text: "agent/x", code: true }, projectName: "harness", taskTitle: "Fix malloc", repositoryName: "api", createdAt: at(5) });
    assert.equal(toRow(n, NOW).label, `Unread. Needs you. devops wants to push agent/x. harness › Fix malloc · api. ${fullDate(at(5))}, 5m ago.`);
    assert.equal(toRow(notification({ createdAt: at(0), readAt: at(0), level: "info", title: "Note" }), NOW).label, `harness Note. harness › Ship the thing. ${fullDate(at(0))}, just now.`);
  });

  test("the Mark read button is named for what the row says", () => {
    assert.equal(toRow(notification({ actor: agent, verb: "wants to push", object: { text: "agent/x", code: true } }), NOW).markLabel, "Mark “wants to push agent/x” read");
    assert.equal(toRow(notification({ title: "A note" }), NOW).markLabel, "Mark “A note” read");
  });

  test("lists the sessions of an interruption and of a commit check, and shows the body only without a list", () => {
    const interrupted = toRow(notification({ body: "b", interruptedSessions: [{ sessionId: "s1", projectId: "p", taskId: "t", taskTitle: "Ship", agentName: "go", link: "/s1", state: "failed", reason: "no room" }] }), NOW);
    assert.deepEqual([interrupted.sessions, interrupted.showBody], [[{ key: "s1", label: "Ship · go", href: "/s1", state: "failed", reason: "no room" }], false]);
    const listed = toRow(notification({ listedSessions: [{ sessionId: "l1", taskId: "", taskTitle: "", agentName: "go", link: "/l1", state: "repaired" }] }), NOW);
    assert.deepEqual(listed.listed, [{ key: "l1", label: "No task · go", href: "/l1", state: "repaired", stateWord: "repaired" }]);
    assert.equal(toRow(notification({ body: "just text" }), NOW).showBody, true);
    assert.equal(toRow(notification(), NOW).showBody, false);
  });

  test("the time-dependent words are empty until the panel knows the time, and the rest does not change", () => {
    const n = notification({ actor: agent, createdAt: at(5) });
    const before = toRow(n, null);
    assert.deepEqual([before.clock, before.fullDate, before.age], ["", "", ""]);
    assert.equal(before.label, "Unread. Needs you. devops harness Review. harness › Ship the thing.".replace("harness Review", `${n.title}`));
    const after = toRow(n, NOW);
    assert.deepEqual([after.clock, after.age], [hhmm(at(5)), "5m"]);
  });
});

function fullDate(d: Date): string {
  const part = (o: Intl.DateTimeFormatOptions) => d.toLocaleDateString("en-GB", o);
  return `${part({ weekday: "short" })} ${part({ day: "numeric" })} ${part({ month: "short" })} ${d.getFullYear()}, ${hhmm(d)}`;
}
