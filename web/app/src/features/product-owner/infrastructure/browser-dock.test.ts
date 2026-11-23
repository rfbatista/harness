import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { browserDockMemory, type StorageLike } from "./browser-dock";

const storage = (initial: Record<string, string> = {}): StorageLike & { held: Record<string, string> } => {
  const held = { ...initial };
  return { held, getItem: (k) => held[k] ?? null, setItem: (k, v) => void (held[k] = v) };
};

describe("what the browser remembers of the panel", () => {
  test("forgets nothing it was told: open, width, and the thread of each project", () => {
    const memory = browserDockMemory(storage());
    memory.save("p1", { open: true, width: 31, threadId: "t1" });
    memory.save("p2", { open: true, width: 31, threadId: "t2" });
    assert.deepEqual(memory.load("p1"), { open: true, width: 31, threadId: "t1" });
    assert.deepEqual(memory.load("p2"), { open: true, width: 31, threadId: "t2" });
    assert.equal(memory.load("p3").threadId, null);
  });

  test("a fresh browser has the panel closed at the default width", () => {
    assert.deepEqual(browserDockMemory(storage()).load("p1"), { open: false, width: 27, threadId: null });
  });

  test("text that does not read is forgotten, not an error", () => {
    for (const bad of ["not json", "[]", '{"open":"yes"}', "null"]) {
      assert.deepEqual(browserDockMemory(storage({ "harness.productOwner": bad })).load("p1"), { open: false, width: 27, threadId: null });
    }
  });

  test("a storage that refuses a write leaves the panel working", () => {
    const memory = browserDockMemory({ getItem: () => null, setItem: () => { throw new Error("full"); } });
    assert.doesNotThrow(() => memory.save("p1", { open: true, width: 27, threadId: null }));
  });
});
