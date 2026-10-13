import { assert, file, test } from "./shared/testing/test.js";
import { wheelPages } from "./wheel-pages.js";

file("wheel-pages");

const pager = () => wheelPages({ step: () => 300, line: () => 15 });

test("a scroll becomes a page key once it has gone half the pane", () => {
  const p = pager();
  assert.deepEqual([p.add(-100), p.add(-100), p.add(-100)], [null, null, "PageUp"]);
  assert.deepEqual([p.add(150), p.add(150)], [null, "PageDown"]);
});

test("a change of direction starts over, and a fling is one key", () => {
  const p = pager();
  assert.equal(p.add(-250), null);
  assert.equal(p.add(100), null, "the 250 up is dropped");
  assert.equal(p.add(200), "PageDown");
  assert.equal(p.add(5000), "PageDown");
  assert.equal(p.add(100), null, "nothing carried over from the fling");
});

test("wheels that count lines or pages", () => {
  const p = pager();
  assert.deepEqual([p.add(-3, 1), p.add(-3, 1), p.add(-3, 1), p.add(-3, 1), p.add(-3, 1)], [null, null, null, null, null]);
  assert.equal(p.add(-6, 1), "PageUp", "20 lines of 15px is 300px");
  assert.equal(p.add(1, 2), "PageDown");
  assert.equal(p.add(0), null);
  p.add(-200);
  p.reset();
  assert.equal(p.add(-200), null);
});
