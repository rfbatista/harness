import { assert, file, test } from "../testing/test.js";
import { SKELETON_MIN_SHOWN_MS, SKELETON_SHOW_AFTER_MS, skeletonRule } from "./skeletonRule.js";

file("shared/domain/skeletonRule");

/** Timers on a virtual clock: nothing runs until the test advances it. */
function virtualTimers() {
  let at = 0;
  let next = 1;
  const queue = new Map();
  return {
    now: () => at,
    setTimeout: (fn, ms) => {
      queue.set(next, { fn, due: at + ms });
      return next++;
    },
    clearTimeout: (id) => queue.delete(id),
    get pending() {
      return queue.size;
    },
    advance(ms) {
      const to = at + ms;
      for (;;) {
        const due = [...queue.entries()].filter(([, t]) => t.due <= to).sort((a, b) => a[1].due - b[1].due)[0];
        if (!due) break;
        queue.delete(due[0]);
        at = due[1].due;
        due[1].fn();
      }
      at = to;
    },
  };
}

function setup() {
  const timers = virtualTimers();
  const seen = [];
  const rule = skeletonRule({ timers, onChange: (s) => seen.push({ at: timers.now(), ...s }) });
  return { timers, rule, seen };
}

test("the rule's numbers are the glossary's: show after 120 ms, keep 300 ms", () => {
  assert.equal(SKELETON_SHOW_AFTER_MS, 120);
  assert.equal(SKELETON_MIN_SHOWN_MS, 300);
});

test("a load that ends within 120 ms never shows a skeleton and leaves no timer", () => {
  const { timers, rule, seen } = setup();
  rule.begin();
  timers.advance(50);
  rule.end();
  timers.advance(1000);
  assert.deepEqual(seen, []);
  assert.equal(timers.pending, 0);
});

test("a load of 200 ms shows the skeleton from 120 ms until 420 ms, then fades the content in", () => {
  const { timers, rule, seen } = setup();
  rule.begin();
  timers.advance(119);
  assert.equal(rule.visible, false);
  timers.advance(1);
  assert.equal(rule.visible, true);
  timers.advance(80);
  rule.end();
  assert.equal(rule.visible, true, "still shown: it has been up for 80 ms only");
  timers.advance(219);
  assert.equal(rule.visible, true);
  timers.advance(1);
  assert.equal(rule.visible, false);
  assert.deepEqual(seen, [
    { at: 120, visible: true, fade: false },
    { at: 420, visible: false, fade: true },
  ]);
});

test("a load that outlasts 420 ms keeps the skeleton until it ends, then hides it at once", () => {
  const { timers, rule, seen } = setup();
  rule.begin();
  timers.advance(1000);
  assert.equal(rule.visible, true);
  rule.end();
  assert.equal(rule.visible, false);
  assert.deepEqual(seen.at(-1), { at: 1000, visible: false, fade: true });
  assert.equal(timers.pending, 0);
});

test("an error removes the skeleton at once, even inside its 300 ms, without a fade", () => {
  const { timers, rule, seen } = setup();
  rule.begin();
  timers.advance(150);
  rule.fail();
  assert.equal(rule.visible, false);
  assert.deepEqual(seen.at(-1), { at: 150, visible: false, fade: false });
  assert.equal(timers.pending, 0);
});

test("an error before 120 ms never shows it", () => {
  const { timers, rule, seen } = setup();
  rule.begin();
  timers.advance(40);
  rule.fail();
  timers.advance(1000);
  assert.deepEqual(seen, []);
  assert.equal(timers.pending, 0);
});

test("a rule stopped while waiting never shows the skeleton and leaves no timer", () => {
  const { timers, rule, seen } = setup();
  rule.begin();
  timers.advance(30);
  rule.stop();
  timers.advance(1000);
  assert.deepEqual(seen, []);
  assert.equal(timers.pending, 0);
});

test("a rule stopped while shown leaves no timer behind", () => {
  const { timers, rule } = setup();
  rule.begin();
  timers.advance(200);
  rule.end();
  rule.stop();
  assert.equal(timers.pending, 0);
});

test("a second load starts the rule afresh: its own 120 ms, its own 300 ms", () => {
  const { timers, rule, seen } = setup();
  rule.begin();
  timers.advance(130);
  rule.end();
  timers.advance(300);
  assert.equal(rule.visible, false);
  timers.advance(500);
  rule.begin();
  timers.advance(119);
  assert.equal(rule.visible, false);
  timers.advance(1);
  assert.equal(rule.visible, true);
  assert.equal(seen.filter((s) => s.visible).length, 2);
});

test("begin while a load is already running does not restart the wait", () => {
  const { timers, rule } = setup();
  rule.begin();
  timers.advance(100);
  rule.begin();
  timers.advance(20);
  assert.equal(rule.visible, true);
});

test("running tells whether a load is under way, for a caller that must not cut a skeleton's 300 ms short", () => {
  const { timers, rule } = setup();
  assert.equal(rule.running, false);
  rule.begin();
  assert.equal(rule.running, true);
  timers.advance(200);
  rule.end();
  assert.equal(rule.running, false, "ended: the skeleton waits out its 300 ms on its own");
  assert.equal(rule.visible, true);
});

test("end() resolves when the skeleton is gone, so a caller shows its content then and nothing stacks under the skeleton", async () => {
  const { timers, rule } = setup();
  const log = [];
  rule.begin();
  timers.advance(200);
  rule.end().then(() => log.push("content"));
  await Promise.resolve();
  assert.deepEqual(log, [], "the skeleton has 220 ms left");
  timers.advance(220);
  await Promise.resolve();
  assert.deepEqual(log, ["content"]);
  assert.equal(rule.visible, false);
});

test("end() resolves at once when no skeleton was shown", async () => {
  const { timers, rule } = setup();
  rule.begin();
  timers.advance(50);
  let done = false;
  rule.end().then(() => (done = true));
  await Promise.resolve();
  assert.equal(done, true);
});

test("a skeleton that has been up 300 ms resolves end() at once", async () => {
  const { timers, rule } = setup();
  rule.begin();
  timers.advance(1000);
  let done = false;
  rule.end().then(() => (done = true));
  await Promise.resolve();
  assert.equal(done, true);
  assert.equal(rule.visible, false);
});

test("stopping while end() waits resolves it, so no caller waits for ever", async () => {
  const { timers, rule } = setup();
  rule.begin();
  timers.advance(200);
  let done = false;
  rule.end().then(() => (done = true));
  rule.stop();
  await Promise.resolve();
  assert.equal(done, true);
  assert.equal(timers.pending, 0);
});
