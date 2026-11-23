import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { ProductOwnerError } from "./ports";
import { memoryProductOwner } from "../infrastructure/memory-product-owner";
import { memoryDockMemory, memoryNavigator, memoryScreen } from "../testing/memory-dock-adapters";
import { OwnerDock, MAX_WIDTH, MIN_WIDTH } from "./owner-dock";

const setup = (opts: { narrow?: boolean } = {}) => {
  const server = memoryProductOwner();
  server.seedProject("p1", true);
  server.seedProject("p2", true);
  const memory = memoryDockMemory();
  const screen = memoryScreen(opts.narrow ?? false);
  const navigator = memoryNavigator();
  const dock = new OwnerDock({ gateway: server, events: server, memory, screen, navigator });
  return { server, memory, screen, navigator, dock };
};
const settled = () => new Promise((resolve) => setImmediate(resolve));

describe("the dock", () => {
  test("starts closed, with the switch not yet known, and has no project until one is bound", () => {
    const { dock } = setup();
    assert.deepEqual({ ...dock.state }, { projectId: "", open: false, docked: false, width: 27, threadId: null, enabled: null, narrow: false, focusComposer: 0, focusToggle: 0, error: "" });
  });

  test("binding a project reads its switch, and follows it", async () => {
    const { dock, server } = setup();
    dock.bind("p1");
    await settled();
    assert.equal(dock.state.projectId, "p1");
    assert.equal(dock.state.enabled, true);
    server.publish("p1", { type: "settings", settings: { projectId: "p1", enabled: false } });
    assert.equal(dock.state.enabled, false);
    server.publish("p2", { type: "settings", settings: { projectId: "p2", enabled: true } });
    assert.equal(dock.state.enabled, false);
  });

  test("toggling opens and closes the panel and remembers it", () => {
    const { dock, memory } = setup();
    dock.bind("p1");
    dock.toggle();
    assert.equal(dock.state.open, true);
    assert.equal(dock.state.docked, true);
    assert.equal(memory.load("p1").open, true);
    dock.toggle();
    assert.equal(dock.state.open, false);
    assert.equal(memory.load("p1").open, false);
  });

  test("opening asks for focus on the composer; closing from inside asks for it on the toggle, closing by the toggle does not", () => {
    const { dock } = setup();
    dock.bind("p1");
    dock.toggle();
    assert.equal(dock.state.focusComposer, 1);
    dock.close({ returnFocus: true });
    assert.equal(dock.state.focusToggle, 1);
    dock.toggle();
    dock.toggle();
    assert.equal(dock.state.focusToggle, 1);
  });

  test("reopens from what the browser remembered, on the project it binds", () => {
    const { dock, memory } = setup();
    memory.preset("p1", { open: true, width: 31, threadId: "t7" });
    dock.bind("p1");
    assert.equal(dock.state.open, true);
    assert.equal(dock.state.width, 31);
    assert.equal(dock.state.threadId, "t7");
    dock.bind("p2");
    assert.equal(dock.state.threadId, null);
    assert.equal(dock.state.open, true, "the panel stays open from project to project");
  });

  test("the width is kept between 22 and 40 rem and remembered", () => {
    const { dock, memory } = setup();
    dock.bind("p1");
    dock.setWidth(10);
    assert.equal(dock.state.width, MIN_WIDTH);
    dock.setWidth(99);
    assert.equal(dock.state.width, MAX_WIDTH);
    dock.setWidth(30.5);
    assert.equal(memory.load("p1").width, 30.5);
  });

  test("on a narrow screen the toggle opens the page, and the panel never docks", () => {
    const { dock, navigator, screen, memory } = setup({ narrow: true });
    memory.preset("p1", { open: true, width: 27, threadId: null });
    dock.bind("p1");
    assert.equal(dock.state.docked, false);
    dock.toggle();
    assert.deepEqual(navigator.opened, ["/projects/p1/product-owner"]);
    screen.setNarrow(false);
    assert.equal(dock.state.docked, true);
  });

  test("the toggle does nothing without a project", () => {
    const { dock, navigator } = setup();
    dock.toggle();
    assert.equal(dock.state.open, false);
    assert.deepEqual(navigator.opened, []);
  });

  test("an unreadable switch leaves it unknown and the panel usable", async () => {
    const { dock, server } = setup();
    server.failWith(new ProductOwnerError("NETWORK_ERROR", "offline"));
    dock.bind("p1");
    await settled();
    assert.equal(dock.state.enabled, null);
  });

  test("turning it on flips the switch; a refusal is shown and the switch stays", async () => {
    const { dock, server } = setup();
    server.seedProject("p3", false);
    dock.bind("p3");
    await settled();
    assert.equal(dock.state.enabled, false);
    server.failWith(new ProductOwnerError("NETWORK_ERROR", "offline"));
    await dock.turnOn();
    assert.equal(dock.state.enabled, false);
    assert.match(dock.state.error, /offline/);
    server.failWith(null);
    await dock.turnOn();
    assert.equal(dock.state.enabled, true);
    assert.equal(dock.state.error, "");
  });

  test("reads the switch again when the stream comes back after a drop", async () => {
    const { dock, server } = setup();
    dock.bind("p1");
    await settled();
    server.publish("p1", { type: "settings", settings: { projectId: "p1", enabled: false } });
    server.setStatus("paused");
    await server.setEnabled("p1", true).then(() => {});
    server.setStatus("resynced");
    await settled();
    assert.equal(dock.state.enabled, true);
  });

  test("a stale release does not undo a later bind, and the last release leaves no follower and no listener", async () => {
    const { dock, server, screen } = setup();
    const first = dock.bind("p1");
    const second = dock.bind("p2");
    first();
    assert.equal(dock.state.projectId, "p2");
    assert.equal(server.followers, 1);
    second();
    assert.equal(dock.state.projectId, "");
    assert.equal(server.followers, 0);
    assert.equal(screen.listeners, 0);
  });

  test("a read that answers after the project changed is dropped", async () => {
    const { dock, server } = setup();
    server.seedProject("p3", false);
    dock.bind("p3");
    dock.bind("p1");
    await settled();
    assert.equal(dock.state.enabled, true);
  });
});
