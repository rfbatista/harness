import { assert, file, test } from "../../../shared/testing/test.js";
import { makeReview, R0 } from "../testing/fixtures.js";
import { isPending, noteProblem, upsertReview } from "./review.js";

file("reviews/domain/review");

test("requests stay newest first; a changed one replaces itself", () => {
  const at = (m) => new Date(R0.getTime() + m * 60_000);
  let list = upsertReview([], makeReview({ id: "a", createdAt: at(1) }));
  list = upsertReview(list, makeReview({ id: "b", createdAt: at(2) }));
  list = upsertReview(list, makeReview({ id: "a", createdAt: at(1), state: "approved" }));
  assert.deepEqual(list.map((r) => `${r.id}:${r.state}`), ["b:pending", "a:approved"]);
  assert.equal(isPending(list[0]), true);
});

test("asking for changes needs a note; approving does not", () => {
  assert.equal(noteProblem("approved", ""), "");
  assert.ok(noteProblem("changes_requested", "   ").length > 0);
  assert.equal(noteProblem("changes_requested", "Rename it."), "");
});
