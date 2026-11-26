import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { HOLD_MS, initialTracker, settle, type Level } from "./level";

/** Feeds one sample per second from `from`, returns the level after each. */
function run(shares: number[], from = initialTracker()): Level[] {
  let t = from;
  const out: Level[] = [];
  shares.forEach((share, i) => {
    t = settle(t, share, i * 1000);
    out.push(t.level);
  });
  return out;
}

describe("memory level", () => {
  test("the first sample paints at once, whatever the level", () => {
    assert.deepEqual(run([0.9]), ["near"]);
    assert.deepEqual(run([1.2]), ["over"]);
    assert.deepEqual(run([0.5]), ["normal"]);
  });

  test("near starts at 80 % and over at 100 %", () => {
    assert.deepEqual(run([0.79]), ["normal"]);
    assert.deepEqual(run([0.8]), ["near"]);
    assert.deepEqual(run([0.99]), ["near"]);
    assert.deepEqual(run([1.0]), ["over"]);
  });

  test("a state holds 3 s before it can change again", () => {
    assert.equal(HOLD_MS, 3000);
    // near at 0 s, over wanted at 1 s and 2 s, allowed from 3 s
    assert.deepEqual(run([0.85, 1.1, 1.1, 1.1]), ["near", "near", "near", "over"]);
  });

  test("noise around 80 % does not flicker: it enters at 80 and leaves only below 75, after the hold", () => {
    assert.deepEqual(run([0.81, 0.79, 0.82, 0.78, 0.8, 0.79, 0.81, 0.78]), Array(8).fill("near"));
    assert.deepEqual(run([0.81, 0.74, 0.74, 0.74, 0.74]), ["near", "near", "near", "normal", "normal"]);
  });

  test("over leaves below 95 %, not at 99 %", () => {
    assert.deepEqual(run([1.1, 0.99, 0.99, 0.99, 0.99]), Array(5).fill("over"));
    assert.deepEqual(run([1.1, 0.9, 0.9, 0.9, 0.9]), ["over", "over", "over", "near", "near"]);
  });

  test("a fall far below leaves over for normal in one step", () => {
    assert.deepEqual(run([1.1, 0.4, 0.4, 0.4]), ["over", "over", "over", "normal"]);
  });
});
