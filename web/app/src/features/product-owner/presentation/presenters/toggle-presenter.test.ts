import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { OwnerDock } from "../../application/owner-dock";
import { memoryProductOwner } from "../../infrastructure/memory-product-owner";
import { memoryDockMemory, memoryNavigator, memoryScreen } from "../../testing/memory-dock-adapters";
import { TogglePresenter } from "./toggle-presenter";
import { Conversation } from "../../application/conversation";
import { ThreadList } from "../../application/thread-list";
import { AboutPresenter } from "./about-presenter";
import { PanelPresenter } from "./panel-presenter";
import { ThreadListPresenter } from "./thread-list-presenter";
import { ThreadPresenter } from "./thread-presenter";

const panelOf = (dock: OwnerDock, server: ReturnType<typeof memoryProductOwner>) => {
  const conversation = new Conversation({ gateway: server, events: server, onThread: (id) => dock.setThreadId(id) });
  const list = new ThreadList({ gateway: server, events: server });
  const clock = { now: () => Date.now() };
  const parts = { list, threads: new ThreadListPresenter(list, conversation, clock), about: new AboutPresenter(conversation, list) };
  return new PanelPresenter(dock, conversation, new ThreadPresenter(conversation, { projectName: "harness", noTasks: false }, { count: () => 0, find: () => null }), parts);
};

const setup = () => {
  const server = memoryProductOwner();
  server.seedProject("p1", true);
  server.seedProject("off", false);
  const screen = memoryScreen();
  const navigator = memoryNavigator();
  const dock = new OwnerDock({ gateway: server, events: server, memory: memoryDockMemory(), screen, navigator });
  return { server, screen, navigator, dock, toggle: new TogglePresenter(dock), panel: panelOf(dock, server) };
};
const settled = () => new Promise((resolve) => setImmediate(resolve));

describe("the toggle", () => {
  test("is not offered on a page of no project", () => {
    assert.equal(setup().toggle.viewModel.visible, false);
  });

  test("is a pressed button while the panel is open, named for the product owner", async () => {
    const { dock, toggle } = setup();
    dock.bind("p1");
    await settled();
    assert.deepEqual({ ...toggle.viewModel }, { visible: true, pressed: false, off: false, label: "Product owner", accessibleName: "Product owner", shortcut: "Alt+Shift+O", focusRequest: 0 });
    toggle.toggle();
    assert.equal(toggle.viewModel.pressed, true);
  });

  test("shows the product owner off: its accessible name says so", async () => {
    const { dock, toggle } = setup();
    dock.bind("off");
    await settled();
    assert.equal(toggle.viewModel.off, true);
    assert.equal(toggle.viewModel.accessibleName, "Product owner (off)");
  });

  test("on a narrow screen it is not pressed: it opens the page", () => {
    const { dock, toggle, screen, navigator } = setup();
    screen.setNarrow(true);
    dock.bind("p1");
    toggle.toggle();
    assert.equal(toggle.viewModel.pressed, false);
    assert.deepEqual(navigator.opened, ["/projects/p1/product-owner"]);
  });

  test("takes focus back when the panel is closed from inside", () => {
    const { dock, toggle, panel } = setup();
    dock.bind("p1");
    toggle.toggle();
    panel.close();
    assert.equal(toggle.viewModel.focusRequest, 1);
  });

  test("the shortcut Alt+Shift+O toggles it, and nothing else does", () => {
    const { dock, toggle } = setup();
    dock.bind("p1");
    assert.equal(toggle.handleKey({ code: "KeyO", altKey: true, shiftKey: true, ctrlKey: false, metaKey: false }), true);
    assert.equal(toggle.viewModel.pressed, true);
    assert.equal(toggle.handleKey({ code: "KeyO", altKey: true, shiftKey: false, ctrlKey: false, metaKey: false }), false);
    assert.equal(toggle.handleKey({ code: "KeyO", altKey: true, shiftKey: true, ctrlKey: true, metaKey: false }), false);
    assert.equal(toggle.viewModel.pressed, true);
  });
});

describe("the panel", () => {
  test("is not shown closed, and docks open with the remembered width", () => {
    const { dock, panel } = setup();
    dock.bind("p1");
    assert.equal(panel.viewModel.docked, false);
    dock.open();
    assert.equal(panel.viewModel.docked, true);
    assert.equal(panel.viewModel.width, 27);
    assert.equal(panel.viewModel.projectId, "p1");
  });

  test("lets the composer be used while the product owner is on or not yet known", async () => {
    const { dock, panel } = setup();
    dock.bind("p1");
    assert.equal(panel.viewModel.composerDisabled, false);
    await settled();
    assert.equal(panel.viewModel.state, "on");
    assert.equal(panel.viewModel.composerDisabled, false);
  });

  test("says it is off, disables the composer, offers Turn it on and the Settings link", async () => {
    const { dock, panel } = setup();
    dock.bind("off");
    await settled();
    assert.equal(panel.viewModel.state, "off");
    assert.equal(panel.viewModel.composerDisabled, true);
    assert.equal(panel.viewModel.composerPlaceholder, "Turn the product owner on to send a message");
    assert.equal(panel.viewModel.settingsHref, "/projects/off/settings#product-owner");
    await panel.turnOn();
    assert.equal(panel.viewModel.state, "on");
    assert.equal(panel.viewModel.composerDisabled, false);
    assert.equal(panel.viewModel.composerPlaceholder, "Ask about the project, or shape a feature");
  });

  test("closing it from inside asks the toggle for focus; opening asks the composer", () => {
    const { dock, panel } = setup();
    dock.bind("p1");
    dock.open();
    assert.equal(panel.viewModel.focusRequest, 1);
    panel.close();
    assert.equal(panel.viewModel.docked, false);
  });

  test("Escape closes it", () => {
    const { dock, panel } = setup();
    dock.bind("p1");
    dock.open();
    assert.equal(panel.handleKey({ key: "Escape" }), true);
    assert.equal(panel.viewModel.docked, false);
    assert.equal(panel.handleKey({ key: "Enter" }), false);
  });

  test("resizing keeps it between 22 and 40 rem, and arrow keys on the handle resize by one rem", () => {
    const { dock, panel } = setup();
    dock.bind("p1");
    dock.open();
    panel.resize(35);
    assert.equal(panel.viewModel.width, 35);
    panel.resize(5);
    assert.equal(panel.viewModel.width, 22);
    assert.equal(panel.handleResizerKey({ key: "ArrowLeft" }), true);
    assert.equal(panel.viewModel.width, 23);
    assert.equal(panel.handleResizerKey({ key: "ArrowRight" }), true);
    assert.equal(panel.viewModel.width, 22);
    assert.equal(panel.handleResizerKey({ key: "a" }), false);
  });
});

describe("the panel's conversation", () => {
  test("is read while the panel is docked, on the thread the browser remembered, and let go when it closes", async () => {
    const { dock, panel, server } = setup();
    server.seedThread("p1", { threadId: "t1" }, [{ kind: "message", itemId: "m1", at: new Date(), author: "person", text: "Q", streaming: false, stopped: false }]);
    dock.bind("p1");
    dock.setThreadId("t1");
    assert.equal(server.followers, 1, "the dock's own follower only while the panel is closed");
    dock.open();
    await settled();
    assert.equal(server.followers, 3, "the dock, the conversation and the thread list");
    assert.equal(panel.thread.viewModel.rows.length, 1);
    panel.close();
    assert.equal(server.followers, 1);
  });

  test("remembers the thread a first message starts", async () => {
    const { dock, panel, server } = setup();
    dock.bind("p1");
    dock.open();
    panel.thread.setDraft("Hello");
    await panel.thread.send();
    assert.ok(dock.state.threadId);
    assert.equal(dock.state.threadId, server.itemsOf(dock.state.threadId!) && dock.state.threadId);
  });

  test("a closed panel, and a destroyed one, leave nothing on the stream", async () => {
    const { dock, panel, server } = setup();
    dock.bind("p1");
    dock.open();
    panel.destroy();
    assert.equal(server.followers, 1);
    dock.open();
    dock.close();
    assert.equal(server.followers, 1);
  });

  test("follows the switch: the composer is disabled while it is off", async () => {
    const { dock, panel } = setup();
    dock.bind("off");
    dock.open();
    await settled();
    assert.equal(panel.thread.viewModel.composer.disabled, true);
    await panel.turnOn();
    assert.equal(panel.thread.viewModel.composer.disabled, false);
  });
});
