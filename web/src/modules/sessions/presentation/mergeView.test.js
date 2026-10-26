import { assert, file, test } from "../../../shared/testing/test.js";
import { METHODS, consequence, resultView } from "./mergeView.js";

file("sessions/presentation/mergeView");

const offer = { available: true, branch: "agent/x", into: "develop", reason: "", ahead: 4, uncommittedChanges: 0, suggestedMessage: "", targets: [{ name: "develop", isDefault: false, checkedOut: false, dirty: false }] };

test("the methods are a merge commit and a squash, never a rebase", () => {
  assert.deepEqual(METHODS.map((m) => m.value), ["merge", "squash"]);
});

test("a squash sentence says one commit, that the branch keeps its commits, and that nothing is pushed", () => {
  assert.equal(consequence(offer, "squash"), "Squashes 4 commits from agent/x into one new commit on develop. agent/x keeps its commits. The main checkout is not touched. Nothing is pushed.");
});

test("a conflict names the files and says to ask the agent", () => {
  const view = resultView({ merged: false, method: "merge", into: "main", head: "", code: "MERGE_CONFLICT", message: "conflict", files: ["a.go", "b.go"] });
  assert.equal(view.detail, "Conflicts in a.go, b.go. Nothing changed. Ask the agent to merge main into its branch, then merge again.");
  assert.equal(view.role, "alert");
});

test("a success says nothing was pushed and the session is unchanged", () => {
  const view = resultView({ merged: true, method: "squash", into: "develop", head: "0123456789abcdef", code: "", message: "" });
  assert.deepEqual([view.title, view.detail, view.note, view.role], ["Merged into develop", "develop is now at 01234567.", "Nothing was pushed. The session, its worktree and the task status are unchanged.", "status"]);
});
