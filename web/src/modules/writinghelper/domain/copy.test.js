import { assert, file, test } from "../../../shared/testing/test.js";
import { startLead, startPlaceholder, startQuestion } from "./copy.js";

file("writinghelper/domain/copy");

test("an empty instruction file asks what the file should make agents do", () => {
  assert.equal(startQuestion("instruction_file"), "What should this file make agents do?");
  assert.equal(
    startLead("instruction_file", "instruction file", ""),
    "The instruction file is empty. Say in a few words what agents should do differently once they read it.",
  );
  assert.equal(
    startLead("instruction_file", "instruction file", "path"),
    "The instruction file is empty. Say in a few words what agents should do differently once they read it, or start from the path.",
  );
  assert.ok(startPlaceholder("instruction_file").startsWith("e.g. "));
});

test("the other kinds keep their copy", () => {
  assert.equal(startQuestion("comment"), "What do you want to say?");
  assert.equal(startQuestion("task_description"), "What is this task about?");
  assert.equal(startLead("comment", "comment", ""), "The comment is empty. Say in a few words what you want to tell the agents.");
});
