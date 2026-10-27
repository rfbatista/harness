import { assert, file, test } from "../../../shared/testing/test.js";
import {
  MESSAGE_REQUIRED,
  NAME_REQUIRED,
  duplicateRule,
  groupRules,
  issueText,
  previewBar,
  ruleSummary,
  ruleTitle,
  withKind,
} from "./hooks.js";

file("architecture/domain/hooks");

const edit = { id: "stay", kind: "edit_outside_layer", mode: "blocking", message: "m" };
const cmd = { id: "vet", kind: "command", at: "before_commit", command: "go vet ./...", mode: "warning", message: "m", paths: "^internal/", timeout_seconds: 60 };

test("a group says how many rules it holds, and the list row says what the rule does", () => {
  const groups = groupRules([edit, cmd]);
  assert.deepEqual(groups.map((g) => [g.key, g.rules.length]), [["before_tool_and_commit", 1], ["before_commit", 1]]);
  assert.equal(ruleSummary(edit), "Edits outside the layers the agent may change");
  assert.equal(ruleSummary(cmd), "go vet ./...");
  assert.equal(ruleSummary({ kind: "command", command: "" }), "No command yet");
  assert.equal(ruleTitle(edit), "stay");
  assert.equal(ruleTitle({ id: "  " }), "New rule");
});

test("switching a rule's kind keeps what both kinds hold and drops what only the other holds", () => {
  const toEdit = withKind(cmd, "edit_outside_layer");
  assert.equal(toEdit.kind, "edit_outside_layer");
  assert.equal(toEdit.id, "vet");
  assert.equal(toEdit.mode, "warning");
  for (const k of ["at", "command", "paths", "tools", "timeout_seconds"]) assert.equal(k in toEdit, false, k);
  const toCmd = withKind({ ...edit, agents: ["go-developer"] }, "command");
  assert.equal(toCmd.kind, "command");
  assert.equal(toCmd.at, "before_commit");
  assert.equal(toCmd.command, "");
  assert.deepEqual(toCmd.agents, ["go-developer"]);
});

test("a duplicate is a command rule with a fresh id", () => {
  const copy = duplicateRule([edit, cmd], cmd);
  assert.equal(copy.id, "vet-copy");
  assert.equal(copy.command, "go vet ./...");
  assert.equal(duplicateRule([edit, cmd, copy], cmd).id, "vet-copy-2");
});

test("an empty name or message is refused in the design's words, and other problems keep the server's", () => {
  assert.equal(MESSAGE_REQUIRED, "A message is required. Say what was refused and what to do instead.");
  assert.equal(NAME_REQUIRED, "Give the rule a name.");
  assert.equal(issueText({ id: "", message: "m" }, "id", "bad"), NAME_REQUIRED);
  assert.equal(issueText({ id: "a", message: "  " }, "message", "message is required"), MESSAGE_REQUIRED);
  assert.equal(issueText({ id: "a", message: "m" }, "id", "id already used"), "id already used");
  assert.equal(issueText({ id: "a", message: "m" }, "command", "A command rule needs a command."), "A command rule needs a command.");
});

test("the preview's bar names the agent, the lifecycle point and the tools", () => {
  assert.equal(previewBar(edit), "agent: plain · Before a tool call and at commit");
  assert.equal(previewBar({ ...cmd, agents: ["go-developer"] }), "agent: go-developer · Before a commit");
});
