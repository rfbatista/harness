import { assert, file, test } from "../testing/test.js";
import {
  OWN_WORKTREE, branchLabel, sessionStatusWord, worktreeBadge, worktreeDescription, worktreeKindWord, worktreeTone,
} from "./runWorktree.js";

file("shared/domain/runWorktree");

const main = { key: "main", kind: "main", branch: "main", dir: "/r", sessionId: "", agent: "", sessionStatus: "" };
const other = { key: "session:s2", kind: "session", branch: "agent/other", dir: "/w", sessionId: "s2", agent: "go-developer", sessionStatus: "running" };

test("a run worktree is named by its kind, branch, agent and status", () => {
  assert.equal(OWN_WORKTREE, "own");
  assert.equal(worktreeKindWord(main), "Main checkout");
  assert.equal(worktreeKindWord({ ...other, agent: "" }), "Session");
  assert.equal(branchLabel(""), "detached HEAD");
  assert.equal(worktreeBadge(other), "go-developer · agent/other");
  assert.equal(worktreeDescription(main), "Main checkout · your own working copy");
  assert.equal(worktreeDescription({ ...other, sessionStatus: "done" }), "go-developer · ended");
  assert.equal(sessionStatusWord("waiting_approval"), "waiting approval");
  assert.equal(worktreeTone(other), "signal");
  assert.equal(worktreeTone(main), "");
});
