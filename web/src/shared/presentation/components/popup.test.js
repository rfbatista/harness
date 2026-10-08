import { assert, file, test } from "../../testing/test.js";
import { defaultEnv, floating, placement, typeahead } from "./popup.js";

file("shared/presentation/popup");

const rect = (left, top, width, height) => ({ left, top, width, height, right: left + width, bottom: top + height });
const view = { width: 1000, height: 800 };

test("a popup sits 4px below its trigger, at least as wide", () => {
  const p = placement(rect(100, 100, 200, 32), { width: 150, height: 300 }, view);
  assert.equal(p.placement, "bottom");
  assert.equal(p.top, 136);
  assert.equal(p.left, 100);
  assert.equal(p.minWidth, 200);
});

test("it flips above when the room below is short and there is more above", () => {
  const p = placement(rect(100, 700, 200, 32), { width: 200, height: 300 }, view);
  assert.equal(p.placement, "top");
  assert.equal(p.top, 700 - 4 - 300);
});

test("it stays below when above has no more room, and shrinks to fit", () => {
  const p = placement(rect(100, 380, 200, 32), { width: 200, height: 600 }, view);
  assert.equal(p.placement, "bottom");
  assert.equal(p.maxHeight, 800 - 412 - 4 - 8);
});

test("it never leaves the viewport sideways; end alignment lines up the end edges", () => {
  assert.equal(placement(rect(950, 100, 40, 32), { width: 300, height: 100 }, view).left, 1000 - 300 - 8);
  assert.equal(placement(rect(600, 100, 100, 32), { width: 300, height: 100 }, view, { align: "end" }).left, 400);
});

test("type-ahead matches a prefix, cycles a repeated letter and forgets after 500ms", () => {
  let t = 0;
  const ta = typeahead(() => t);
  const labels = ["alpha", "beta", "bravo", "charlie", "bob"];
  assert.equal(ta.find("b", labels, 0), 1);
  t += 100;
  assert.equal(ta.find("r", labels, 1), 2, "br → bravo");
  t += 600;
  assert.equal(ta.find("b", labels, 2), 4, "a fresh b after bravo → bob");
  t += 100;
  assert.equal(ta.find("b", labels, 4), 1, "b again cycles round to beta");
  assert.equal(ta.find("x", labels, 0), -1);
});

test("type-ahead skips entries that are not usable", () => {
  const ta = typeahead(() => 0);
  assert.equal(ta.find("b", ["beta", "bob"], -1, (i) => i !== 0), 1);
});

test("only one popup is open at a time", () => {
  const a = document.createElement("div");
  const b = document.createElement("div");
  const anchor = document.createElement("button");
  document.body.append(anchor, a, b);
  const closed = [];
  const env = defaultEnv();
  const fa = floating(a, { anchor, env, onDismiss: () => (closed.push("a"), fa.close()) });
  const fb = floating(b, { anchor, env, onDismiss: () => (closed.push("b"), fb.close()) });
  try {
    fa.open();
    assert.ok(a.matches(":popover-open"), "a is in the top layer");
    fb.open();
    assert.deepEqual(closed, ["a"]);
    assert.ok(!a.matches(":popover-open"));
    assert.ok(b.matches(":popover-open"));
  } finally {
    fb.close();
    a.remove();
    b.remove();
    anchor.remove();
  }
});

test("a click outside dismisses it; one inside does not", () => {
  const el = document.createElement("div");
  const inner = document.createElement("span");
  el.append(inner);
  const anchor = document.createElement("button");
  const outside = document.createElement("p");
  document.body.append(anchor, el, outside);
  let dismissed = 0;
  const f = floating(el, { anchor, env: defaultEnv(), onDismiss: () => dismissed++ });
  try {
    f.open();
    inner.dispatchEvent(new PointerEvent("pointerdown", { bubbles: true }));
    anchor.dispatchEvent(new PointerEvent("pointerdown", { bubbles: true }));
    assert.equal(dismissed, 0);
    outside.dispatchEvent(new PointerEvent("pointerdown", { bubbles: true }));
    assert.equal(dismissed, 1);
  } finally {
    f.close();
    el.remove();
    anchor.remove();
    outside.remove();
  }
});
