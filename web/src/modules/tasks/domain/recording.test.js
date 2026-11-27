import { assert, file, test } from "../../../shared/testing/test.js";
import { choiceOf, decodeRecording, enabledOf, resolution } from "./recording.js";

file("tasks/domain/recording");

const answer = { task_id: "t1", enabled: true, explicit: true, source: "task", default: false, default_source: "project" };

test("a task's own flag is on, off, or inherit when it sets nothing", () => {
  assert.equal(choiceOf(true), "on");
  assert.equal(choiceOf(false), "off");
  assert.equal(choiceOf(null), "inherit");
  assert.deepEqual(["on", "off", "inherit"].map(enabledOf), [true, false, null]);
});

test("the resolution line says what holds, where it comes from and what the default is", () => {
  const rec = decodeRecording(answer);
  assert.equal(resolution(rec, "on"), "on, from this task; project default off");
  assert.equal(resolution(rec, "off"), "off, from this task; project default off");
  assert.equal(resolution(rec, "inherit"), "off, from the project default");
  assert.equal(resolution({ ...rec, default: true, defaultSource: "harness" }, "inherit"), "on, from the harness default");
  assert.equal(resolution({ ...rec, default: true, defaultSource: "repository" }, "off"), "off, from this task; repository default on");
});

test("an answer that is not a recording is refused", () => {
  assert.throws(() => decodeRecording(null));
  assert.throws(() => decodeRecording({ task_id: "t1", explicit: "yes", default: false }));
});

test("an answer without the default still reads: the line names only the task's choice", () => {
  const rec = decodeRecording({ task_id: "t1", enabled: true, explicit: true, source: "task", default: false, default_source: "" });
  assert.equal(resolution(rec, "on"), "on, from this task");
});
