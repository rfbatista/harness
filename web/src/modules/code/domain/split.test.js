import { assert, file, test } from "../../../shared/testing/test.js";
import { clampSplit, defaultSplit, parseSplit, splitBounds, stepSplit } from "./split.js";

file("code/domain/split");

test("the file list is at least 12rem and at most half of the layout", () => {
  assert.deepEqual(splitBounds(1000, 16), { min: 19.2, max: 50 });
  assert.equal(clampSplit(5, 1000, 16), 19.2);
  assert.equal(clampSplit(80, 1000, 16), 50);
  assert.equal(clampSplit(33, 1000, 16), 33);
  // a layout so narrow that 12rem is over half: the half wins
  assert.deepEqual(splitBounds(300, 16), { min: 50, max: 50 });
});

test("the default is the CSS default: a third of the layout, between 14rem and 18rem", () => {
  assert.equal(defaultSplit(1000, 16), 28.8);
  assert.equal(defaultSplit(2000, 16), 14.4);
  assert.equal(defaultSplit(600, 16), 37.3);
});

test("arrows step two points and stay inside the bounds, Home resets, other keys do nothing", () => {
  assert.equal(stepSplit(30, "ArrowRight", 1000, 16), 32);
  assert.equal(stepSplit(30, "ArrowLeft", 1000, 16), 28);
  assert.equal(stepSplit(20, "ArrowLeft", 1000, 16), 19.2);
  assert.equal(stepSplit(49, "ArrowRight", 1000, 16), 50);
  assert.equal(stepSplit(30, "Home", 1000, 16), null, "null is the default again");
  assert.equal(stepSplit(30, "a", 1000, 16), undefined);
});

test("a stored split is a number of percent; anything else is no choice", () => {
  assert.equal(parseSplit("31.5"), 31.5);
  assert.equal(parseSplit(null), null);
  assert.equal(parseSplit("banana"), null);
  assert.equal(parseSplit("-4"), null);
  assert.equal(parseSplit("140"), null);
  assert.equal(parseSplit(""), null);
});
