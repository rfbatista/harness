import { assert, file, test } from "../../../shared/testing/test.js";
import { toBranchesEvent, toSeed, toSessionBranches } from "../infrastructure/dto.js";
import { branchesLabel, flaggedCount, isFlagged, rowOf } from "./branches.js";

file("sync/domain/branches");

const wire = (name, state, extra = {}) => ({ name, is_session_branch: state === "session", state, base: null, pushed: [], request_ids: [], flagged_at: null, ...extra });

test("the list reads the contract's shape, in the order the server gave", () => {
  const list = toSessionBranches({
    session_id: "s1",
    branches: [
      wire("agent/x", "session", { base: { ref: "main", ahead: 2, behind: 1 } }),
      wire("feature", "approved", { pushed: [{ remote: "origin", remote_branch: "feature", at: "2026-11-08T10:00:00Z", by: "agent" }], request_ids: ["r2", "r1"] }),
      wire("sneaky", "unapproved", { flagged_at: "2026-11-08T11:00:00Z" }),
    ],
  });
  assert.equal(list.sessionId, "s1");
  assert.deepEqual(list.branches.map((b) => b.name), ["agent/x", "feature", "sneaky"]);
  assert.deepEqual(list.branches[0].base, { ref: "main", ahead: 2, behind: 1 });
  assert.equal(list.branches[1].pushed[0].by, "agent");
  assert.deepEqual(list.branches[1].requestIds, ["r2", "r1"]);
  assert.equal(list.branches[2].flaggedAt.toISOString(), "2026-11-08T11:00:00.000Z");
});

test("an unknown state or a branch with no name is a bad response", () => {
  assert.throws(() => toSessionBranches({ session_id: "s1", branches: [wire("a", "weird")] }));
  assert.throws(() => toSessionBranches({ session_id: "s1", branches: [wire("", "approved")] }));
});

test("a feed message carries a list under session_branches and any other kind is none", () => {
  assert.equal(toBranchesEvent({ git_state: {} }), null);
  assert.equal(toBranchesEvent({ session_branches: { session_id: "s1", branches: [wire("a", "session")] } }).sessionId, "s1");
});

test("the seed carries every session's list beside the git states", () => {
  const seed = toSeed({ project_id: "p1", git_states: [], session_branches: [{ session_id: "s1", branches: [wire("a", "session")] }] });
  assert.equal(seed.sessionBranches.length, 1);
  assert.deepEqual(toSeed({ project_id: "p1" }).sessionBranches, []);
});

test("a branch is flagged when unapproved or pushed around the gate; the count is of those", () => {
  const list = toSessionBranches({
    session_id: "s1",
    branches: [wire("a", "session", { flagged_at: "2026-11-08T11:00:00Z" }), wire("b", "approved"), wire("c", "unapproved"), wire("d", "denied")],
  }).branches;
  assert.deepEqual(list.map(isFlagged), [true, false, true, false]);
  assert.equal(flaggedCount(list), 2);
  assert.equal(branchesLabel(1), "1 branch");
  assert.equal(branchesLabel(5), "5 branches");
});

test("a row says its state, its counts and who pushed where", () => {
  const [row] = toSessionBranches({
    session_id: "s1",
    branches: [wire("f", "approved", { base: { ref: "main", ahead: 1, behind: 0 }, pushed: [{ remote: "origin", remote_branch: "f", at: "2026-11-08T10:00:00Z", by: "unapproved" }] })],
  }).branches.map(rowOf);
  assert.equal(row.stateLabel, "approved");
  assert.equal(row.countsLabel, "1 ahead, 0 behind main");
  assert.equal(row.pushed[0].line, "pushed to origin/f by no approval");
  assert.equal(rowOf(toSessionBranches({ session_id: "s", branches: [wire("a", "session")] }).branches[0]).stateLabel, "session branch");
});

test("Push is offered on the session branch and an approved one, when the repository has a remote", () => {
  const remotes = [{ name: "origin" }, { name: "fork" }];
  const offers = (state, ctx) => rowOf(toSessionBranches({ session_id: "s1", branches: [wire("b", state)] }).branches[0], ctx).offersPush;
  assert.deepEqual(["session", "approved"].map((s) => offers(s, { remotes })), [true, true]);
  assert.deepEqual(["denied", "deleted", "unapproved"].map((s) => offers(s, { remotes })), [false, false, false]);
  assert.equal(offers("approved", { remotes: [] }), false);
  assert.equal(offers("approved", {}), false);
});

test("the Push question names the remote, the remote branch and the commit count, worded by what they are counted against", () => {
  const branch = toSessionBranches({ session_id: "s1", branches: [wire("feature-x", "approved")] }).branches[0];
  const ask = (preview) => rowOf(branch, { remotes: [{ name: "origin" }, { name: "fork" }], push: { branch: "feature-x", remote: "fork", preview, failure: null } });
  assert.equal(ask({ head: "h", commits: 1, commitsAgainst: "remote", remoteBranch: "feature-x" }).question, "Push 1 commit of feature-x to fork/feature-x?");
  assert.equal(ask({ head: "h", commits: 3, commitsAgainst: "base", remoteBranch: "review/x" }).question, "Push feature-x to fork/review/x? 3 commits ahead of its base; fork/review/x is not known here.");
  const loading = ask(null);
  assert.deepEqual([loading.confirming, loading.previewLoading, loading.offersPush, loading.remoteOptions.map((o) => o.selected)], [true, true, false, [false, true]]);
});

test("a pushable row of a repository with no remote says so; before the remotes are read it says nothing", () => {
  const row = (state, ctx) => rowOf(toSessionBranches({ session_id: "s1", branches: [wire("b", state)] }).branches[0], ctx);
  assert.deepEqual([row("approved", { remotes: [] }).noRemote, row("approved", {}).noRemote, row("denied", { remotes: [] }).noRemote], [true, false, false]);
});
