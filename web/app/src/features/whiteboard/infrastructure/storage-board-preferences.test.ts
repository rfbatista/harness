import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { memoryStorage } from "../testing/memory-storage";
import { browserStorage, cameraKey, positionsKey, storageBoardPreferences, viewKey, type KeyValueStorage } from "./storage-board-preferences";

const throwing: KeyValueStorage = {
  getItem: () => {
    throw new Error("blocked");
  },
  setItem: () => {
    throw new Error("quota");
  },
  removeItem: () => {
    throw new Error("blocked");
  },
};

describe("board preferences over storage", () => {
  test("keys sit under the harness: prefix, one per task and kind", () => {
    assert.equal(viewKey("t1"), "harness:agents-view:t1");
    assert.equal(cameraKey("t1"), "harness:whiteboard-camera:t1");
    assert.equal(positionsKey("t1"), "harness:whiteboard-positions:t1");
  });

  test("positions, camera and view round-trip per task", () => {
    const storage = memoryStorage();
    const prefs = storageBoardPreferences(storage);
    prefs.savePositions("t1", { s1: { x: 10, y: 20 } });
    prefs.saveCamera("t1", { x: -5, y: 7, scale: 0.5 });
    prefs.saveView("t1", "whiteboard");
    assert.deepEqual(prefs.positions("t1"), { s1: { x: 10, y: 20 } });
    assert.deepEqual(prefs.camera("t1"), { x: -5, y: 7, scale: 0.5 });
    assert.equal(prefs.view("t1"), "whiteboard");
    assert.deepEqual(prefs.positions("t2"), {});
    assert.equal(prefs.camera("t2"), null);
    assert.equal(prefs.view("t2"), null);
  });

  test("saving no positions removes the key rather than storing {}", () => {
    const storage = memoryStorage();
    const prefs = storageBoardPreferences(storage);
    prefs.savePositions("t1", { s1: { x: 1, y: 2 } });
    prefs.savePositions("t1", {});
    assert.equal(storage.entries.has(positionsKey("t1")), false);
  });

  test("corrupt stored positions read as none: not JSON, wrong shape, a non-number coordinate", () => {
    for (const bad of ["{oops", "null", "[]", '{"positions":[]}', '{"positions":{"s1":{"x":"1","y":2}}}', '{"positions":{"s1":{"x":1}}}', '{"positions":{"s1":5}}', '{"positions":{"s1":{"x":null,"y":2}}}']) {
      const prefs = storageBoardPreferences(memoryStorage({ [positionsKey("t1")]: bad }));
      assert.deepEqual(prefs.positions("t1"), {}, bad);
    }
  });

  test("corrupt stored camera reads as none: not JSON, missing field, scale out of the zoom limits", () => {
    for (const bad of ["{oops", "[]", '{"x":1,"y":2}', '{"x":1,"y":2,"scale":"1"}', '{"x":1,"y":2,"scale":0.01}', '{"x":1,"y":2,"scale":50}', '{"x":null,"y":2,"scale":1}']) {
      const prefs = storageBoardPreferences(memoryStorage({ [cameraKey("t1")]: bad }));
      assert.equal(prefs.camera("t1"), null, bad);
    }
  });

  test("a stored view other than list or whiteboard reads as none", () => {
    for (const bad of ["", "timeline", "Whiteboard", '"whiteboard"']) {
      assert.equal(storageBoardPreferences(memoryStorage({ [viewKey("t1")]: bad })).view("t1"), null, bad);
    }
  });

  test("blocked or missing storage never throws: reads give defaults, writes are dropped", () => {
    for (const storage of [throwing, null]) {
      const prefs = storageBoardPreferences(storage);
      assert.deepEqual(prefs.positions("t1"), {});
      assert.equal(prefs.camera("t1"), null);
      assert.equal(prefs.view("t1"), null);
      prefs.savePositions("t1", { s1: { x: 1, y: 2 } });
      prefs.savePositions("t1", {});
      prefs.saveCamera("t1", { x: 0, y: 0, scale: 1 });
      prefs.saveView("t1", "list");
    }
  });

  test("browserStorage gives nothing when localStorage is absent or throws on access", () => {
    const descriptor = Object.getOwnPropertyDescriptor(globalThis, "localStorage");
    try {
      Object.defineProperty(globalThis, "localStorage", { configurable: true, get: () => { throw new Error("denied"); } });
      assert.equal(browserStorage(), null);
      Object.defineProperty(globalThis, "localStorage", { configurable: true, value: undefined });
      assert.equal(browserStorage(), null);
    } finally {
      if (descriptor) Object.defineProperty(globalThis, "localStorage", descriptor);
      else delete (globalThis as { localStorage?: unknown }).localStorage;
    }
  });
});
