import { assert, file, test } from "../../../shared/testing/test.js";
import { makeDraft, makePullRequest, P0 } from "../testing/fixtures.js";
import { canCreate, displayState, formFromDraft, formProblems, hasProblems, isOpen, newerOf } from "./pullRequest.js";

file("pullrequests/domain/pullRequest");

test("an open PR marked draft reads as draft; the others as their state", () => {
  assert.deepEqual(
    [makePullRequest(), makePullRequest({ draft: true }), makePullRequest({ state: "merged", draft: true }), makePullRequest({ state: "closed" })].map(displayState),
    ["open", "draft", "merged", "closed"],
  );
  assert.deepEqual([isOpen(makePullRequest()), isOpen(makePullRequest({ state: "merged" })), isOpen(null)], [true, false, false]);
});

test("a create is offered with no open PR and nothing in the way, a merged or closed one notwithstanding", () => {
  assert.ok(canCreate(makeDraft(), null));
  assert.ok(canCreate(makeDraft(), makePullRequest({ state: "merged" })));
  assert.ok(canCreate(makeDraft(), makePullRequest({ state: "closed" })));
  assert.ok(!canCreate(makeDraft(), makePullRequest()));
  assert.ok(!canCreate(makeDraft({ blockers: ["PR_NO_COMMITS"] }), null));
  assert.ok(!canCreate(null, null));
});

test("the form starts from the draft's suggestions, not as a draft PR", () => {
  assert.deepEqual(formFromDraft(makeDraft()), { title: "Create pull request", body: makeDraft().body, baseBranch: "main", draft: false });
});

test("the form needs a title; an empty base is the draft's", () => {
  const problems = formProblems({ title: "  ", body: "", baseBranch: "", draft: false });
  assert.deepEqual(problems, { title: "A pull request needs a title.", baseBranch: "" });
  assert.ok(hasProblems(problems));
  assert.ok(!hasProblems(formProblems({ title: "T", body: "", baseBranch: "", draft: false })));
});

test("of two PRs of a workspace the newer shows; the same one replaces itself", () => {
  const merged = makePullRequest({ number: 7, state: "merged" });
  const later = makePullRequest({ number: 9, createdAt: new Date(P0.getTime() + 60_000) });
  assert.equal(newerOf(null, merged), merged);
  assert.equal(newerOf(merged, later), later);
  assert.equal(newerOf(later, merged), later, "an old one's late news does not hide the newer");
  const closed = makePullRequest({ number: 9, state: "closed", createdAt: later.createdAt });
  assert.equal(newerOf(later, closed), closed);
});
