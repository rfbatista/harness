import { assert, file, test } from "../../../shared/testing/test.js";
import { makeTotals } from "../testing/fixtures.js";
import { totalsChip } from "./totals.js";

file("usage/presentation/totals");

test("a task's or session's totals read like the Usage page: tokens and the estimated cost", () => {
  const chip = totalsChip(makeTotals({ requests: 3, input: 1200, output: 340, cacheWrite: 50, cacheRead: 9000, total: 10_590, costUsd: 0.4249 }));
  assert.equal(chip.text, "11K tokens · $0.42 estimated");
  assert.ok(chip.title.includes("1,200 input"), chip.title);
  assert.ok(chip.title.includes("340 output"), chip.title);
  assert.ok(chip.title.includes("50 cache write"), chip.title);
  assert.ok(chip.title.includes("9,000 cache read"), chip.title);
  assert.ok(chip.title.includes("Estimated cost $0.42"), chip.title);
});

test("an unpriced total is a dash, never $0", () => {
  const chip = totalsChip(makeTotals({ input: 10, total: 10, costUsd: null, unpriced: 10 }));
  assert.equal(chip.text, "10 tokens · — estimated");
  assert.ok(!chip.text.includes("$"));
  assert.ok(chip.title.includes("no price"), chip.title);
});

test("no totals, no chip", () => {
  assert.equal(totalsChip(null), null);
  assert.equal(totalsChip(undefined), null);
});
