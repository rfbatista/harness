import { assert, file, test } from "../../../shared/testing/test.js";
import { ruleEventRow } from "./events.js";

file("architecture/domain/events");

const ev = { id: "e1", rule_id: "r", outcome: "blocked", at: "2026-01-01T00:00:00Z", session_id: "s1", task_id: "t1" };

test("an event with a task links to its session", () => {
  assert.equal(ruleEventRow(ev, "p1", new Date()).sessionHref, "/projects/p1/tasks/t1?session=s1");
});

test("an event with no task has no link, never /tasks/undefined", () => {
  assert.equal(ruleEventRow({ ...ev, task_id: undefined }, "p1", new Date()).sessionHref, "");
  assert.equal(ruleEventRow({ ...ev, task_id: "" }, "p1", new Date()).sessionHref, "");
});
