import { assert, file, test } from "../../../shared/testing/test.js";
import { makeState, T0 } from "../testing/fixtures.js";
import { popoverOf } from "./popoverView.js";

file("sync/presentation/popoverView");

const session = (o = {}) => ({ id: "s1", ticketId: "t1", agentId: "a1", repositoryId: "r1", workspaceId: "ws1", branch: "agent/sync-1234", ...o });
const world = {
  agentNames: { a1: "go-developer", a2: "docs-writer" },
  repositoryNames: { r1: "blog", r2: "api" },
  projectId: "p1",
  ticketId: "t1",
  now: T0,
};
const byWorkspace = (...states) => Object.fromEntries(states.map((s) => [s.workspaceId, s]));
const popover = (sessions, states, extra = {}) => popoverOf({ ...world, sessions, states: byWorkspace(...states), ...extra });

test("the button shows only when the task has a session with a workspace", () => {
  assert.equal(popover([session()], [makeState()]).shown, true);
  assert.equal(popover([session({ workspaceId: "" })], []).shown, false);
  assert.equal(popover([], []).shown, false);
  assert.equal(popover([session({ ticketId: "other" })], [makeState()]).shown, false, "another task's session is not this task's worktree");
});

test("one row per session worktree, in the sessions' order, with agent, branch, both comparisons and the uncommitted words", () => {
  const second = makeState({ workspaceId: "ws2", sessionId: "s2", repositoryId: "r2", branch: "agent/docs-77aa", upstream: "none", base: { ref: "main", ahead: 5, behind: 0 }, uncommitted: { tracked: 1, untracked: 0, staged: 0 } });
  const view = popover([session(), session({ id: "s2", agentId: "a2", repositoryId: "r2", workspaceId: "ws2" })], [makeState({ upstream: "ahead" }), second]);
  const read = (r) => [r.workspaceId, r.agent, r.branch, r.baseState, r.baseAhead, r.baseBehind, r.upstreamState, r.upstreamWord, r.uncommitted];
  assert.deepEqual(view.rows.map(read), [
    ["ws1", "go-developer", "agent/sync-1234", "diverged", 3, 8, "ahead", "", "3 changed · 2 new"],
    ["ws2", "docs-writer", "agent/docs-77aa", "ahead", 5, 0, "none", "not pushed", "1 changed"],
  ]);
  assert.deepEqual([view.rows[0].upstreamRemote, view.rows[0].upstreamAhead, view.rows[0].upstreamBehind], ["origin", 3, 0]);
  assert.equal(popover([session()], [makeState({ uncommitted: { tracked: 0, untracked: 0, staged: 0 } })]).rows[0].uncommitted, "clean");
});

test("a session with no agent is named plain claude; a worktree not read yet has its row without states", () => {
  const view = popover([session({ agentId: "" })], []);
  assert.deepEqual([view.rows[0].agent, view.rows[0].branch, view.rows[0].hasState, view.rows[0].offersPush], ["plain claude", "agent/sync-1234", false, false]);
});

test("a row links the session's Code tab", () => {
  const view = popover([session({ id: "s 1" })], [makeState()]);
  assert.equal(view.rows[0].codeHref, "/projects/p1/tasks/t1/sessions/s%201/code");
});

test("Push is offered per row exactly when the branch is ahead or unpushed with commits, and asks the question first", () => {
  const offers = (upstream, o = {}) => popover([session()], [makeState({ upstream, ...o })]).rows[0].offersPush;
  assert.deepEqual(["ahead", "none"].map((u) => offers(u)), [true, true]);
  assert.deepEqual(["in_sync", "behind", "diverged", "gone", "unknown"].map((u) => offers(u)), [false, false, false, false, false]);
  assert.equal(offers("none", { base: { ref: "main", ahead: 0, behind: 0 } }), false, "nothing to push");
  const row = popover([session()], [makeState({ upstream: "ahead" })], { confirming: "ws1" }).rows[0];
  assert.deepEqual([row.confirming, row.showsPush, row.question, row.pushLabel], [true, false, "Push 3 commits to origin/agent/sync-1234?", "Push 3 commits"]);
  assert.deepEqual([popover([session()], [makeState({ upstream: "ahead" })]).rows[0].confirming, popover([session()], [makeState({ upstream: "ahead" })]).rows[0].showsPush], [false, true]);
});

test("a gone worktree has no Push and says so", () => {
  const row = popover([session()], [makeState({ gone: true })]).rows[0];
  assert.deepEqual([row.gone, row.offersPush], [true, false]);
});

test("the foot has one Fetch per repository of the task, named, and the oldest fetch age", () => {
  const two = [
    makeState({ upstream: "ahead", fetchedAt: new Date(T0.getTime() - 12 * 60000) }),
    makeState({ workspaceId: "ws2", sessionId: "s2", repositoryId: "r2", upstream: "ahead", fetchedAt: new Date(T0.getTime() - 90 * 60000) }),
    makeState({ workspaceId: "ws3", sessionId: "s3", repositoryId: "r1", upstream: "ahead", fetchedAt: new Date(T0.getTime() - 5 * 60000) }),
  ];
  const sessions = [session(), session({ id: "s2", repositoryId: "r2", workspaceId: "ws2" }), session({ id: "s3", workspaceId: "ws3" })];
  const view = popover(sessions, two);
  assert.deepEqual(view.fetches.map((f) => [f.repositoryId, f.label]), [["r1", "↻ Fetch blog"], ["r2", "↻ Fetch api"]]);
  assert.deepEqual([view.fresh.line, view.fresh.old], ["Fetched 1 h ago", true]);
  const one = popover([session()], [two[0]]);
  assert.deepEqual([one.fetches.map((f) => f.label), one.fresh.old], [["↻ Fetch blog"], false]);
});

test("a repository fetching reads Fetching… and dims the counts; a never-fetched repository says so and is old", () => {
  const view = popover([session()], [makeState()], { fetching: { r1: true } });
  assert.deepEqual([view.busy, view.fetches[0].label, view.fetches[0].busy], [true, "Fetching…", true]);
  assert.equal(view.rows[0].upstreamLoading, true);
  const never = popover([session()], [makeState({ fetchedAt: null })]);
  assert.deepEqual([never.fresh.line, never.fresh.old], ["Not fetched yet", true]);
});

test("a repository without a name is still fetched", () => {
  const view = popover([session({ repositoryId: "r9" })], [makeState({ repositoryId: "r9" })]);
  assert.deepEqual(view.fetches.map((f) => f.label), ["↻ Fetch"]);
});

test("a refused push is on its row and a refused fetch on its repository", () => {
  const push = { tone: "danger", lead: "origin/x has 2 commits this branch does not have, so nothing was pushed." };
  const fetch = { tone: "banner", lead: "No credential stored for github.com." };
  const view = popover([session()], [makeState()], { pushFailure: { ws1: push }, fetchFailure: { r1: fetch } });
  assert.deepEqual([view.rows[0].refusal, view.fetches[0].refusal], [push, fetch]);
  assert.deepEqual([view.rows[0].refusalStrip, view.rows[0].refusalBanner, view.fetches[0].refusalStrip, view.fetches[0].refusalBanner], [true, false, false, true]);
  const calm = popover([session()], [makeState()]);
  assert.deepEqual([calm.rows[0].refusal, calm.fetches[0].refusal], [null, null]);
});
