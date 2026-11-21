// The contract every NavigationSignal adapter keeps (Contract: Harness server ↔
// Web UI — the top bar island, §4).

import { describe, test } from "node:test";
import assert from "node:assert/strict";
import type { NavigationSignal } from "../application/ports";

export interface NavigationSignalHarness {
  signal: NavigationSignal;
  /** The person moves to `path` by an in-place navigation. */
  navigate(path: string): void;
}

export function navigationSignalContract(name: string, make: () => NavigationSignalHarness): void {
  describe(`NavigationSignal contract: ${name}`, () => {
    test("delivers the path of each navigation, in order", () => {
      const h = make();
      const seen: string[] = [];
      h.signal.subscribe((path) => seen.push(path));
      h.navigate("/projects/p1");
      h.navigate("/agents");
      assert.deepEqual(seen, ["/projects/p1", "/agents"]);
    });

    test("delivers to every subscriber", () => {
      const h = make();
      const a: string[] = [];
      const b: string[] = [];
      h.signal.subscribe((path) => a.push(path));
      h.signal.subscribe((path) => b.push(path));
      h.navigate("/models");
      assert.deepEqual([a, b], [["/models"], ["/models"]]);
    });

    test("delivers nothing after unsubscribe", () => {
      const h = make();
      const seen: string[] = [];
      const stop = h.signal.subscribe((path) => seen.push(path));
      stop();
      h.navigate("/models");
      assert.deepEqual(seen, []);
    });
  });
}
