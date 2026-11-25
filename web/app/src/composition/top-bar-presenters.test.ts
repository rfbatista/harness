import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { topBarJson } from "../features/navigation/testing/fixtures";
import { OwnerDock } from "../features/product-owner/application/owner-dock";
import { memoryProductOwner } from "../features/product-owner/infrastructure/memory-product-owner";
import { memoryDockMemory, memoryNavigator, memoryScreen } from "../features/product-owner/testing/memory-dock-adapters";
import { buildTopBar } from "./top-bar-presenters";

/** Counts the listeners the dock holds. */
function counted(dock: OwnerDock): { listeners: number } {
  const count = { listeners: 0 };
  const subscribe = dock.subscribe;
  dock.subscribe = (listener) => {
    count.listeners += 1;
    const stop = subscribe(listener);
    return () => {
      count.listeners -= 1;
      stop();
    };
  };
  return count;
}

describe("a top bar that another frame replaces", () => {
  test("leaves no listener on the document's dock once it is released", () => {
    const server = memoryProductOwner();
    server.seedProject("p1", true);
    const dock = new OwnerDock({ gateway: server, events: server, memory: memoryDockMemory(), screen: memoryScreen(), navigator: memoryNavigator() });
    const held = counted(dock);
    const adapters = { pageOpener: { open: () => {} }, navigator: memoryNavigator(), notifications: {} as never, dock };
    const props = { ...topBarJson({ notifications: null }), notifications: null };
    for (let frame = 0; frame < 3; frame += 1) {
      const topBar = buildTopBar(props, adapters);
      assert.equal(held.listeners, 1, "the toggle follows the dock while its frame is shown");
      topBar.release();
      assert.equal(held.listeners, 0, "nothing follows the dock once its frame is gone");
    }
  });
});
