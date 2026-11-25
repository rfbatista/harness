import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { Conversation } from "../../application/conversation";
import { OwnerDock } from "../../application/owner-dock";
import { ThreadList } from "../../application/thread-list";
import { memoryProductOwner } from "../../infrastructure/memory-product-owner";
import { memoryDockMemory, memoryNavigator, memoryScreen } from "../../testing/memory-dock-adapters";
import { person, summary } from "../../testing/fixtures";
import { OwnerPagePresenter } from "./owner-page-presenter";

const settled = () => new Promise((resolve) => setImmediate(resolve));

const setup = async (opts: { threadId?: string; phone?: boolean } = {}) => {
  const server = memoryProductOwner();
  server.seedProject("p1", true);
  server.seedThread("p1", summary({ threadId: "t1", title: "One" }), [person("Q")]);
  server.seedThread("p1", summary({ threadId: "t2", title: "Two" }));
  const screen = memoryScreen(opts.phone ?? false, opts.phone ?? false);
  const navigator = memoryNavigator();
  const dock = new OwnerDock({ gateway: server, events: server, memory: memoryDockMemory(), screen, navigator });
  dock.bind("p1");
  const conversation = new Conversation({ gateway: server, events: server });
  const list = new ThreadList({ gateway: server, events: server });
  conversation.attach("p1", opts.threadId ?? null);
  list.attach("p1");
  const page = new OwnerPagePresenter({ projectId: "p1", projectName: "harness", threadId: opts.threadId ?? "" }, dock, conversation, list, navigator);
  await settled();
  return { server, screen, navigator, dock, conversation, list, page };
};

describe("the page", () => {
  test("names its project, links back to it and to Settings, and counts the threads", async () => {
    const { page } = await setup();
    assert.deepEqual({ ...page.viewModel }, { projectName: "harness", crumbHref: "/projects/p1", settingsHref: "/projects/p1/settings#product-owner", threadCount: 2, phone: null, notFound: false });
  });

  test("hides the panel while it is open, and gives it back when it goes", async () => {
    const { page, dock } = await setup();
    dock.open();
    assert.equal(dock.state.docked, false);
    page.destroy();
    assert.equal(dock.state.docked, true);
  });

  test("opens the thread its address names, and says so when it is not a thread of this project", async () => {
    const { conversation } = await setup({ threadId: "t1" });
    assert.equal(conversation.state.threadId, "t1");
    const gone = await setup({ threadId: "nope" });
    assert.equal(gone.page.viewModel.notFound, true);
  });

  test("keeps the address in step with the thread on screen", async () => {
    const { page, navigator } = await setup();
    page.openThread("t2");
    await settled();
    assert.deepEqual(navigator.replaced.at(-1), "/projects/p1/product-owner/threads/t2");
    page.newThread();
    assert.deepEqual(navigator.replaced.at(-1), "/projects/p1/product-owner");
  });

  test("moves the thread beside the board without losing it", async () => {
    const { page, dock, navigator } = await setup({ threadId: "t1" });
    page.dockBeside();
    assert.equal(dock.state.threadId, "t1");
    assert.equal(dock.state.open, true);
    assert.equal(navigator.opened.at(-1), "/projects/p1");
  });
});

describe("on a phone", () => {
  test("shows the thread list first, a thread in place of it, and Back returns to the list", async () => {
    const { page } = await setup({ phone: true });
    assert.equal(page.viewModel.phone, "list");
    page.openThread("t1");
    assert.equal(page.viewModel.phone, "thread");
    page.back();
    assert.equal(page.viewModel.phone, "list");
  });

  test("a link to a thread opens the thread", async () => {
    const { page } = await setup({ phone: true, threadId: "t1" });
    assert.equal(page.viewModel.phone, "thread");
  });
});
