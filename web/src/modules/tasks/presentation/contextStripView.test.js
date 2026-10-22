import { assert, file, test } from "../../../shared/testing/test.js";
import { bandOfHash, buttonToFocus } from "./contextStripView.js";

file("tasks/presentation/contextStripView");

test("a fragment that names a band opens that band's panel; any other opens none", () => {
  assert.deepEqual(["#reviews", "#review-rv1", "#feature", "#token-savers"].map(bandOfHash), ["reviews", "reviews", "feature", "token-savers"]);
  assert.deepEqual(["", "#", "#comments", "#features", "#reviewsx"].map(bandOfHash), ["", "", "", "", ""]);
});

test("the arrow keys step along the strip and stop at its ends; Home and End jump; other keys stay put", () => {
  assert.deepEqual(["ArrowRight", "ArrowRight", "ArrowLeft"].map((k, i) => buttonToFocus(3, [0, 2, 0][i], k)), [1, 2, 0]);
  assert.deepEqual([buttonToFocus(3, 1, "Home"), buttonToFocus(3, 1, "End"), buttonToFocus(3, 1, "Enter"), buttonToFocus(3, 1, "ArrowDown")], [0, 2, -1, -1]);
});
