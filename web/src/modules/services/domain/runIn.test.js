import { assert, file, test } from "../../../shared/testing/test.js";
import { MAIN, afterFailedRead, afterPick, afterRead, freshRunIn, hasRunIn, pickKeyOf, pickedWorktree, runInOptions, startKey } from "./runIn.js";

file("services/domain/runIn");

const main = { key: "main", kind: "main", branch: "main", dir: "/r", sessionId: "", agent: "", sessionStatus: "" };
const other = { key: "session:s2", kind: "session", branch: "agent/other", dir: "/w", sessionId: "s2", agent: "go-developer", sessionStatus: "running" };
const ready = (pickedKey = MAIN, worktrees = [main, other]) => ({ worktrees, state: "ready", pickedKey, restored: true, fallback: null });

test("only a command service with a repository has a Run in picker", () => {
  assert.equal(hasRunIn({ kind: "command", repositoryId: "r1" }), true);
  assert.equal(hasRunIn({ kind: "command", repositoryId: "" }), false);
  assert.equal(hasRunIn({ kind: "image", repositoryId: "r1" }), false);
});

test("the pick is kept per service, apart from the App tab's", () => {
  assert.equal(pickKeyOf("s1"), "service-run-worktree:s1");
});

test("before a list arrives, the pick is the main checkout and a start sends none", () => {
  const fresh = freshRunIn();
  assert.deepEqual([fresh.state, fresh.pickedKey, startKey(fresh)], ["loading", MAIN, ""]);
  assert.equal(startKey(afterFailedRead(fresh)), "");
  assert.equal(afterFailedRead(fresh).state, "failed");
});

test("the first list restores the remembered pick silently", () => {
  const { next, clearStored } = afterRead(freshRunIn(), [main, other], { stored: "session:s2" });
  assert.deepEqual([next.state, next.pickedKey, next.fallback, clearStored, startKey(next)], ["ready", "session:s2", null, false, "session:s2"]);
});

test("nothing remembered, or the main checkout, stays on the main checkout", () => {
  assert.equal(afterRead(freshRunIn(), [main, other], { stored: null }).next.pickedKey, MAIN);
  assert.equal(afterRead(freshRunIn(), [main, other], { stored: MAIN }).next.pickedKey, MAIN);
});

test("a remembered worktree that is gone falls back to the main checkout, clears the memory and says so once", () => {
  const { next, clearStored } = afterRead(freshRunIn(), [main], { stored: "session:s2" });
  assert.deepEqual([next.pickedKey, clearStored], [MAIN, true]);
  assert.equal(next.fallback.message, "That worktree no longer exists. Running in the main checkout.");
});

test("a pick that drops out of a later read names what went", () => {
  const { next, clearStored } = afterRead(ready("session:s2"), [main], { stored: "session:s2" });
  assert.deepEqual([next.pickedKey, clearStored], [MAIN, true]);
  assert.equal(next.fallback.message, "The worktree of go-developer on agent/other no longer exists. Running in the main checkout.");
});

test("a silent fall back, after a refusal, shows no second notice", () => {
  const { next } = afterRead(ready("session:s2"), [main], { stored: null, silent: true });
  assert.deepEqual([next.pickedKey, next.fallback], [MAIN, null]);
});

test("a failed re-read leaves the list and the pick as they were", () => {
  const before = ready("session:s2");
  assert.deepEqual(afterFailedRead(before), before);
});

test("picking a listed worktree sets it and clears the notice; an unlisted key changes nothing", () => {
  const withNotice = { ...ready(), fallback: { message: "x" } };
  const { next, clearStored } = afterPick(withNotice, "session:s2");
  assert.deepEqual([next.pickedKey, next.fallback, clearStored], ["session:s2", null, false]);
  assert.equal(afterPick(ready(), "main").clearStored, true);
  assert.equal(afterPick(ready(), "session:zz"), null);
});

test("the options are the main checkout then the sessions, in the server's order, with the words of the App tab", () => {
  const o = runInOptions(ready());
  assert.deepEqual(o.main.map((x) => [x.key, x.label, x.description]), [["main", "main", "Main checkout · your own working copy"]]);
  assert.deepEqual(o.sessions.map((x) => [x.key, x.label, x.description, x.tone]), [["session:s2", "agent/other", "go-developer · running", "signal"]]);
  assert.equal(pickedWorktree(ready("session:s2")).agent, "go-developer");
});
