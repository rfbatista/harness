import { assert, file, test } from "../../../shared/testing/test.js";
import { makeTotals } from "../testing/fixtures.js";
import { costView, estimatedCost, tokenCount, tokens } from "./format.js";

file("usage/domain/format");

test("token counts are compact, and exact where there is room", () => {
  assert.deepEqual([0, 7, 999, 1000, 1234, 12_345, 999_999, 1_500_000, 2_000_000_000].map(tokens), [
    "0",
    "7",
    "999",
    "1K",
    "1.2K",
    "12K",
    "1M",
    "1.5M",
    "2B",
  ]);
  assert.equal(tokenCount(1_234_567), "1,234,567");
});

test("an estimated cost is in dollars; an unpriced one is a dash, never $0", () => {
  assert.equal(estimatedCost(0.4249), "$0.42");
  assert.equal(estimatedCost(1234.5), "$1,234.50");
  assert.equal(estimatedCost(0.004), "<$0.01");
  assert.equal(estimatedCost(0), "$0.00");
  assert.equal(estimatedCost(null), "—");
  assert.equal(estimatedCost(undefined), "—");
});

test("the cost is always labelled estimated, and says what it leaves out", () => {
  const priced = costView(makeTotals({ total: 1200, costUsd: 0.5 }));
  assert.deepEqual([priced.text, priced.label, priced.note], ["$0.50", "Estimated cost", ""]);
  assert.ok(priced.aria.includes("estimated"), priced.aria);

  const unpriced = costView(makeTotals({ total: 1200, costUsd: null, unpriced: 1200 }));
  assert.equal(unpriced.text, "—");
  assert.equal(unpriced.label, "Estimated cost");
  assert.ok(!unpriced.text.includes("$"), "unpriced is never shown as dollars");
  assert.ok(unpriced.note.length > 0, "says why there is no cost");

  const partly = costView(makeTotals({ total: 3000, costUsd: 0.5, unpriced: 1200 }));
  assert.equal(partly.text, "$0.50");
  assert.ok(partly.note.includes("1.2K"), partly.note);
});
