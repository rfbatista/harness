import { assert, file, test } from "../../../shared/testing/test.js";
import { preferredRun, runCommandNameProblem, runState, worktreeBadge, worktreeDescription, worktreeTone } from "./run.js";

file("runs/domain/run");

const at = (s) => new Date(`2026-10-05T12:00:0${s}Z`);

test("the run to show: the newest running one, else the newest", () => {
  const runs = [
    { id: "c", status: "exited", startedAt: at(3) },
    { id: "b", status: "running", startedAt: at(2) },
    { id: "a", status: "running", startedAt: at(1) },
  ];
  assert.equal(preferredRun(runs).id, "b");
  assert.equal(preferredRun([runs[0]]).id, "c");
  assert.equal(preferredRun([]), null);
});

test("how a run's state reads", () => {
  assert.deepEqual(runState({ status: "running" }), { state: "running", word: "running" });
  assert.deepEqual(runState({ status: "stopped" }), { state: "done", word: "stopped" });
  assert.deepEqual(runState({ status: "exited", exitCode: 0 }), { state: "done", word: "exited" });
  assert.deepEqual(runState({ status: "exited", exitCode: 1 }), { state: "failed", word: "exited (code 1)" });
});

test("a run command's name follows the server's rule", () => {
  assert.equal(runCommandNameProblem("server"), "");
  assert.equal(runCommandNameProblem("  web app_2.dev-x  "), "");
  assert.equal(runCommandNameProblem("a".repeat(40)), "");
  assert.equal(runCommandNameProblem("a".repeat(41)), "Up to 40 characters; this one has 41.");
  assert.equal(runCommandNameProblem("web/app"), "Use only letters, digits, spaces and . _ -");
  assert.equal(runCommandNameProblem("-dev"), "Start with a letter or a digit.");
  assert.equal(runCommandNameProblem("   "), "A run command needs a name.");
});

const own = { key: "own", kind: "own", branch: "agent/feed", dir: "/w/own" };
const main = { key: "main", kind: "main", branch: "main", dir: "/w/main" };
const other = { key: "session:s2", kind: "session", branch: "agent/other", agent: "go-developer", sessionStatus: "running" };

test("a run worktree reads as its kind, branch and, for a session, its agent and status", () => {
  assert.equal(worktreeBadge(own), "This session · agent/feed");
  assert.equal(worktreeBadge(main), "Main checkout · main");
  assert.equal(worktreeBadge(other), "go-developer · agent/other");
  assert.equal(worktreeBadge({ ...other, agent: "" }), "Session · agent/other");
  assert.equal(worktreeBadge({ ...main, branch: "" }), "Main checkout · detached HEAD");
  assert.equal(worktreeDescription(own), "This session's worktree");
  assert.equal(worktreeDescription(main), "Main checkout · your own working copy");
  assert.equal(worktreeDescription(other), "go-developer · running");
  assert.equal(worktreeDescription({ ...other, agent: "", sessionStatus: "done" }), "no agent · ended");
  assert.equal(worktreeDescription({ ...other, sessionStatus: "waiting_approval" }), "go-developer · waiting approval");
});

test("a session worktree's dot follows its status; own and main have none", () => {
  assert.equal(worktreeTone(other), "signal");
  assert.equal(worktreeTone({ ...other, sessionStatus: "waiting_approval" }), "attention");
  assert.equal(worktreeTone({ ...other, sessionStatus: "failed" }), "danger");
  assert.equal(worktreeTone({ ...other, sessionStatus: "done" }), "");
  assert.equal(worktreeTone(own), "");
});
