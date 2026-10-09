import { assert, file, test } from "../../../shared/testing/test.js";
import { makeComment } from "../testing/resource-fixtures.js";
import { canSend, counterTone, counterWords, isEdited, isUnchanged, MAX_COMMENT } from "./comments.js";

file("tasks/domain/comments");

test("a comment is edited when updated_at is after created_at", () => {
  assert.ok(!isEdited(makeComment()));
  assert.ok(isEdited(makeComment({ updatedAt: new Date("2026-10-08T12:05:00Z") })));
});

test("a comment can be sent when the trimmed body is 1 to 10,000 runes", () => {
  assert.ok(!canSend(""));
  assert.ok(!canSend("  \n  "));
  assert.ok(canSend(" ok "));
  assert.ok(canSend("é".repeat(MAX_COMMENT)));
  assert.ok(!canSend("é".repeat(MAX_COMMENT + 1)));
});

test("the counter turns amber from 9,000 and red past 10,000", () => {
  assert.equal(counterTone(8_999), "");
  assert.equal(counterTone(9_000), "attention");
  assert.equal(counterTone(10_000), "attention", "exactly the limit can be sent");
  assert.equal(counterTone(10_001), "danger");
  assert.equal(counterWords(9_412), "9,412 / 10,000");
});

test("an edit that only adds spaces around the body changes nothing", () => {
  const c = makeComment({ body: "Use the v2 contract." });
  assert.ok(isUnchanged(c, "  Use the v2 contract.\n"));
  assert.ok(!isUnchanged(c, "Use the v3 contract."));
});
