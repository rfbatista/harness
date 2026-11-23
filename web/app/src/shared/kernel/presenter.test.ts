import { test } from "node:test";
import assert from "node:assert/strict";
import { Presenter } from "./presenter";

class Counter extends Presenter<number> {
  constructor() {
    super(0);
  }
  bump() {
    this.update((n) => n + 1);
  }
}

test("a subscriber gets the current view model at once and every change", () => {
  const counter = new Counter();
  const seen: number[] = [];
  counter.subscribe((n) => seen.push(n));
  counter.bump();
  counter.bump();
  assert.deepEqual(seen, [0, 1, 2]);
  assert.equal(counter.viewModel, 2);
});

test("a subscriber gets nothing after it unsubscribes", () => {
  const counter = new Counter();
  const seen: number[] = [];
  const stop = counter.subscribe((n) => seen.push(n));
  stop();
  counter.bump();
  assert.deepEqual(seen, [0]);
});
