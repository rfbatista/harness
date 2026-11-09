import { assert, file, test } from "../../../shared/testing/test.js";
import {
  BRANCH_ENTRY, MAIN, afterFailedRead, afterPick, afterRead, afterRefusal, branchNameOf, branchOptions, freshRunIn, hasRunIn, isBranchKey, pickAnnouncement, pickKeyOf, pickedWorktree, restartNote, runInOptions, startKey,
} from "./runIn.js";

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

test("a branch key is told apart from a worktree key, and its label drops the prefix", () => {
  assert.equal(isBranchKey("branch:feature/x"), true);
  assert.equal(isBranchKey("branch:origin/feature/x"), true);
  assert.equal(isBranchKey("branch:"), false, "the Branch… entry is not a branch");
  assert.equal(isBranchKey("session:s2"), false);
  assert.equal(branchNameOf("branch:origin/feature/x"), "origin/feature/x");
});

test("the picker ends with a Branch… entry, and a picked branch has an option of its own", () => {
  const plain = runInOptions(ready());
  assert.deepEqual([plain.branchEntry.key, plain.branchEntry.label], [BRANCH_ENTRY, "Branch…"]);
  assert.equal(plain.branch, null);
  const picked = runInOptions({ ...ready(), pickedKey: "branch:feature/x" });
  assert.deepEqual([picked.branch.key, picked.branch.label, picked.branch.description], ["branch:feature/x", "feature/x", "Branch · committed work only"]);
  assert.equal(runInOptions({ ...ready(), pickedKey: "branch:origin/dev" }).branch.description, "Remote branch · fetched when you start");
});

test("a branch can be picked though no list offers it, and a start sends it even before a list arrived", () => {
  const { next, clearStored } = afterPick(ready(), "branch:feature/x");
  assert.deepEqual([next.pickedKey, clearStored], ["branch:feature/x", false]);
  assert.equal(startKey(next), "branch:feature/x");
  assert.equal(startKey({ ...freshRunIn(), pickedKey: "branch:feature/x" }), "branch:feature/x");
  assert.equal(afterPick(ready(), BRANCH_ENTRY), null, "the entry itself is not a pick");
});

test("a re-read of the worktree list keeps a branch pick", () => {
  const { next, clearStored } = afterRead(ready("branch:feature/x"), [main], { stored: "branch:feature/x" });
  assert.deepEqual([next.pickedKey, next.fallback, clearStored], ["branch:feature/x", null, false]);
});

test("the branches become options: local first, remote marked and fetched first, with the truncation line", () => {
  const now = new Date("2026-11-07T14:00:00Z");
  const list = {
    branches: [
      { name: "feature/x", remote: false, sha: "abc1234", subject: "Add the thing", committedAt: "2026-11-07T12:00:00Z" },
      { name: "dev", remote: true, sha: "def5678", subject: "", committedAt: "" },
    ],
    truncated: true,
  };
  const options = branchOptions(list, now);
  assert.deepEqual(options.map((o) => [o.value, o.label, o.group ?? "", o.description ?? "", o.meta ?? "", o.disabled ?? false]), [
    ["branch:feature/x", "feature/x", "Local branches", "abc1234 · Add the thing", "2h ago", false],
    ["branch:origin/dev", "origin/dev", "Remote branches · fetched first", "def5678", "", false],
    ["", "Showing the first 200, type to narrow", "", "", "", true],
  ]);
  assert.equal(branchOptions({ branches: [], truncated: false }, now).length, 0);
});

test("a branch pick is announced as a branch", () => {
  assert.equal(pickAnnouncement("api", { ...ready(), pickedKey: "branch:feature/x" }), "api runs branch feature/x.");
  assert.equal(pickAnnouncement("api", ready("session:s2")), "api runs in go-developer, branch agent/other.");
});

test("a refused branch is forgotten: the pick goes back to the main checkout, with no second notice", () => {
  const { next, clearStored } = afterRefusal({ ...ready(), pickedKey: "branch:gone" });
  assert.deepEqual([next.pickedKey, next.fallback, clearStored], [MAIN, null, true]);
  const same = afterRefusal(ready("session:s2"));
  assert.deepEqual([same.next.pickedKey, same.clearStored], ["session:s2", false], "a refusal for a worktree is the list's business");
});

test("a branch run is restarted at its latest commit, and a worktree run where it ran", () => {
  assert.equal(restartNote("branch:feature/x"), "Runs the same branch again at its latest commit");
  assert.equal(restartNote("session:s2"), "Repeats the run: the same worktree and the same overrides");
  assert.equal(restartNote(""), "Repeats the run: the same worktree and the same overrides");
});
