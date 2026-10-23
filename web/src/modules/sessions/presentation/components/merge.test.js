import { StructuredError } from "../../../../shared/domain/errors.js";
import { mount } from "../../../../shared/testing/alpine.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { merge } from "./merge.js";

file("sessions/presentation/merge");

const AT = new Date("2026-10-23T12:00:00Z");

function make({ offer = { available: true, into: "main", reason: "" }, outcome, last = null } = {}) {
  const calls = [];
  let release = () => {};
  const gateway = {
    async offer(id) {
      calls.push(["offer", id]);
      return offer;
    },
    async merge(id) {
      calls.push(["merge", id]);
      await new Promise((resolve) => (release = resolve));
      if (outcome instanceof Error) throw outcome;
      return outcome;
    },
  };
  const mounted = mount(() => merge({ merge: gateway })({ sessionId: "s1", branch: "agent/x", last }));
  return { ...mounted, calls, release: () => release() };
}

test("a session a merge applies to offers the button, named for the default branch", async () => {
  const { instance, calls } = make();
  assert.equal(instance.asking, true, "until the server answers");
  await instance.init();
  assert.deepEqual([instance.offered, instance.label, instance.cannotMerge, calls], [true, "Merge into main", false, [["offer", "s1"]]]);
});

test("a session a merge does not apply to offers no button, and says why", async () => {
  const { instance } = make({ offer: { available: false, into: "", reason: "MERGE_NO_CHECKOUT" } });
  await instance.init();
  assert.deepEqual([instance.offered, instance.cannotMerge, instance.notOffered], [false, true, true]);
  assert.ok(instance.unavailable.startsWith("No checkout to merge in"), instance.unavailable);
});

test("the button is disabled while the merge runs, and a second press merges nothing more", async () => {
  const { instance, calls, release } = make({ outcome: { at: AT, merged: true, into: "main", head: "abcdef1234567890", code: "", message: "" } });
  await instance.init();
  const running = instance.mergeNow();
  assert.deepEqual([instance.running, instance.cannotMerge, instance.busyAttr], [true, true, "true"]);
  await instance.mergeNow();
  release();
  await running;
  assert.equal(calls.filter(([what]) => what === "merge").length, 1);
  assert.deepEqual([instance.running, instance.resultTitle, instance.resultDetail, instance.resultTone], [false, "Merged into main", "The default branch is now at abcdef12.", "signal"]);
});

test("a refusal shows its reason and code from the answer, and the button is free again", async () => {
  const refusal = new StructuredError("MERGE_CHECKOUT_DIRTY", "the checkout is not clean: untracked files (a.txt)", 409);
  const { instance, release } = make({ outcome: refusal });
  await instance.init();
  const running = instance.mergeNow();
  release();
  await running;
  assert.deepEqual(
    [instance.resultTitle, instance.resultDetail, instance.resultCode, instance.resultTone, instance.running, instance.cannotMerge],
    ["Not merged", "the checkout is not clean: untracked files (a.txt)", "MERGE_CHECKOUT_DIRTY", "danger", false, false],
  );
});

test("the result the session carries shows at once", () => {
  const { instance } = make({ last: { at: AT, merged: false, into: "main", head: "", code: "MERGE_CONFLICT", message: "conflicts in a.txt; aborted" } });
  assert.deepEqual([instance.hasResult, instance.resultTitle, instance.resultDetail, instance.resultCode, instance.resultTone], [true, "Not merged", "conflicts in a.txt; aborted", "MERGE_CONFLICT", "danger"]);
});
