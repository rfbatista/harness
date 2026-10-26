import { StructuredError } from "../../../../shared/domain/errors.js";
import { mount } from "../../../../shared/testing/alpine.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { merge } from "./merge.js";

file("sessions/presentation/merge");

const AT = new Date("2026-10-23T12:00:00Z");

const TARGETS = [
  { name: "main", isDefault: true, checkedOut: true, dirty: false },
  { name: "develop", isDefault: false, checkedOut: false, dirty: false },
  { name: "release", isDefault: false, checkedOut: false, dirty: false },
];

/** An offer as the server answers it for `into`; overrides win. */
const offerFor = (into, overrides = {}) => ({
  available: true,
  branch: "agent/x",
  into,
  reason: "",
  ahead: 3,
  uncommittedChanges: 0,
  suggestedMessage: "Add SSE feed",
  targets: TARGETS,
  ...overrides,
});

/**
 * `offers` maps a target to the offer for it ("" is the default branch);
 * a function answers each call.
 */
function make({ offers = {}, outcome, last = null, target } = {}) {
  const calls = [];
  let release = () => {};
  const gateway = {
    async offer(id, target) {
      calls.push(["offer", id, target]);
      return offers[target] ?? (target ? offerFor(target) : (offers[""] ?? offerFor("main")));
    },
    async merge(id, request) {
      calls.push(["merge", id, request]);
      await new Promise((resolve) => (release = resolve));
      if (outcome instanceof Error) throw outcome;
      return outcome;
    },
  };
  const mounted = mount(() => merge({ merge: gateway })({ sessionId: "s1", branch: "agent/x", last, target }));
  return { ...mounted, calls, release: () => release() };
}

const merged = { at: AT, merged: true, method: "merge", into: "main", head: "abcdef1234567890", code: "", message: "" };

test("a session a merge applies to offers the button, named for the default branch", async () => {
  const { instance, calls } = make();
  assert.equal(instance.asking, true, "until the server answers");
  await instance.init();
  assert.deepEqual([instance.offered, instance.label, instance.cannotMerge, calls], [true, "Merge into main", false, [["offer", "s1", ""]]]);
  assert.equal(instance.target, "main", "the first offer names the default branch");
});

test("the default branch is preselected and asked for by name when the server's first offer is about another", async () => {
  const { instance, calls } = make({ offers: { "": offerFor("develop") } });
  await instance.init();
  assert.deepEqual(calls, [["offer", "s1", ""], ["offer", "s1", "main"]]);
  assert.deepEqual([instance.target, instance.offer.into, instance.refreshing], ["main", "main", false]);
});

test("the Into picker lists the targets with their marks, the default first", async () => {
  const { instance } = make();
  await instance.init();
  assert.deepEqual(instance.targetOptions, [
    { name: "main", label: "main · default · checked out" },
    { name: "develop", label: "develop" },
    { name: "release", label: "release" },
  ]);
  assert.equal(instance.filterable, false, "three branches need no filter");
});

test("a long list of targets gets a filter", async () => {
  const many = Array.from({ length: 9 }, (_, i) => ({ name: `b${i}`, isDefault: i === 0, checkedOut: false, dirty: false }));
  const { instance } = make({ offers: { "": offerFor("b0", { targets: many }) } });
  await instance.init();
  assert.deepEqual([instance.filterable, instance.plain], [true, false]);
});

test("picking another target asks again, and the button and sentence follow it", async () => {
  const { instance, calls } = make({ offers: { develop: offerFor("develop", { ahead: 1 }) } });
  await instance.init();
  assert.equal(instance.consequence, "Merges 3 commits from agent/x into main with a merge commit. main is checked out in the main checkout, so its files change there. Nothing is pushed.");
  instance.target = "develop";
  await instance.targetChanged();
  assert.deepEqual(calls.at(-1), ["offer", "s1", "develop"]);
  assert.equal(instance.label, "Merge into develop");
  assert.equal(instance.consequence, "Merges 1 commit from agent/x into develop with a merge commit. The main checkout is not touched. Nothing is pushed.");
});

test("an answer for an older target is dropped", async () => {
  let release;
  const gateway = {
    async offer(id, target) {
      if (target === "slow") await new Promise((r) => (release = r));
      return offerFor(target || "main");
    },
    async merge() {},
  };
  const { instance } = mount(() => merge({ merge: gateway })({ sessionId: "s1", branch: "agent/x" }));
  await instance.init();
  instance.target = "slow";
  const slow = instance.targetChanged();
  instance.target = "develop";
  await instance.targetChanged();
  release();
  await slow;
  assert.deepEqual([instance.target, instance.offer.into, instance.refreshing], ["develop", "develop", false]);
});

test("the target a session was last merged into stays picked when the panel mounts again", async () => {
  const { instance, calls } = make({ target: "release" });
  await instance.init();
  assert.deepEqual(calls[0], ["offer", "s1", "release"]);
});

test("a session a merge does not apply to offers no button, and says why", async () => {
  const { instance } = make({ offers: { "": offerFor("", { available: false, reason: "MERGE_NO_CHECKOUT", targets: [] }) } });
  await instance.init();
  assert.deepEqual([instance.offered, instance.cannotMerge, instance.notOffered, instance.showButton], [false, true, true, false]);
  assert.ok(instance.unavailable.startsWith("No checkout to merge in"), instance.unavailable);
});

test("nothing to merge is a note, not a button", async () => {
  const { instance } = make({ offers: { "": offerFor("main", { available: false, reason: "MERGE_ALREADY_MERGED", ahead: 0 }) } });
  await instance.init();
  assert.deepEqual([instance.showButton, instance.notOffered], [false, true]);
  assert.equal(instance.unavailable, "Nothing to merge: agent/x has no commits that main lacks.");
});

test("a target that is not a local branch is a note that says to pick another", async () => {
  const { instance } = make({ target: "gone", offers: { gone: offerFor("gone", { available: false, reason: "MERGE_TARGET_NOT_FOUND" }) } });
  await instance.init();
  assert.equal(instance.unavailable, "gone is not a local branch of this repository. Pick another target.");
});

test("a dirty checked-out target shows the button disabled, with the reason beside it", async () => {
  const { instance } = make({ offers: { "": offerFor("main", { available: false, reason: "MERGE_CHECKOUT_DIRTY" }) } });
  await instance.init();
  assert.deepEqual([instance.showButton, instance.cannotMerge, instance.notOffered], [true, true, false]);
  assert.equal(instance.buttonReason, "main is checked out in the main checkout, and it has uncommitted changes. Commit or stash them there, or pick another target.");
  assert.equal(instance.unavailable, "", "the reason is not said twice");
});

test("uncommitted changes in the session's worktree are a warning, not a block", async () => {
  const { instance } = make({ offers: { "": offerFor("main", { uncommittedChanges: 2 }) } });
  await instance.init();
  assert.equal(instance.warning, "2 files in the session's worktree are not committed. The merge leaves them out.");
  assert.equal(instance.cannotMerge, false);
});

test("the form is disabled while the merge runs, and a second press merges nothing more", async () => {
  const { instance, calls, release } = make({ outcome: merged });
  await instance.init();
  const running = instance.mergeNow();
  assert.deepEqual([instance.running, instance.formBusy, instance.cannotMerge, instance.busyAttr], [true, true, true, "true"]);
  await instance.mergeNow();
  release();
  await running;
  assert.equal(calls.filter(([what]) => what === "merge").length, 1);
  assert.deepEqual([instance.running, instance.resultTitle, instance.resultDetail, instance.resultTone], [false, "Merged into main", "main is now at abcdef12.", "signal"]);
});

test("the merge goes into the picked target", async () => {
  const { instance, calls, release } = make({ outcome: { ...merged, into: "develop" } });
  await instance.init();
  instance.target = "develop";
  await instance.targetChanged();
  const running = instance.mergeNow();
  release();
  await running;
  assert.deepEqual(calls.at(-1), ["merge", "s1", { target: "develop", method: "merge", message: "Add SSE feed" }]);
});

test("a refusal shows its reason and code from the answer, and the button is free again", async () => {
  const refusal = new StructuredError("MERGE_CHECKOUT_DIRTY", "the checkout is not clean: untracked files (a.txt)", 409);
  const { instance, release } = make({ outcome: refusal });
  await instance.init();
  const running = instance.mergeNow();
  release();
  await running;
  assert.deepEqual(
    [instance.resultTitle, instance.resultDetail, instance.resultCode, instance.resultTone, instance.resultRole, instance.running, instance.cannotMerge],
    ["Not merged", "the checkout is not clean: untracked files (a.txt)", "MERGE_CHECKOUT_DIRTY", "danger", "alert", false, false],
  );
});

test("the result the session carries shows at once", () => {
  const { instance } = make({ last: { at: AT, merged: false, method: "merge", into: "main", head: "", code: "MERGE_BRANCH_NOT_FOUND", message: "the branch is gone" } });
  assert.deepEqual([instance.hasResult, instance.resultTitle, instance.resultDetail, instance.resultCode, instance.resultTone], [true, "Not merged", "the branch is gone", "MERGE_BRANCH_NOT_FOUND", "danger"]);
});
