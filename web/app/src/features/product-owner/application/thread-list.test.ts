import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { memoryProductOwner } from "../infrastructure/memory-product-owner";
import { summary } from "../testing/fixtures";
import { ProductOwnerError } from "./ports";
import { ThreadList } from "./thread-list";

const settled = () => new Promise((resolve) => setImmediate(resolve));
const ids = (l: ThreadList) => l.state.threads.map((t) => t.threadId);

const setup = () => {
  const server = memoryProductOwner();
  server.seedProject("p1", true);
  server.seedThread("p1", summary({ threadId: "old", lastActivityAt: new Date("2026-11-20T10:00:00Z") }));
  server.seedThread("p1", summary({ threadId: "new", lastActivityAt: new Date("2026-11-23T10:00:00Z") }));
  return { server, list: new ThreadList({ gateway: server, events: server }) };
};

describe("the thread list", () => {
  test("reads the project's threads, newest first", async () => {
    const { list } = setup();
    list.attach("p1");
    assert.equal(list.state.loading, true);
    await settled();
    assert.deepEqual(ids(list), ["new", "old"]);
    assert.equal(list.state.loading, false);
  });

  test("shows a thread that starts, renames and changes its turn, and moves the most recent to the top", async () => {
    const { list, server } = setup();
    list.attach("p1");
    await settled();
    await server.renameThread("old", "Roadmap");
    assert.equal(list.state.threads.find((t) => t.threadId === "old")?.title, "Roadmap");
    await server.startThread("p1", "A fresh question");
    assert.equal(list.state.threads.length, 3);
    server.setTurn("old", "running");
    assert.equal(list.state.threads.find((t) => t.threadId === "old")?.turn, "running");
  });

  test("says why it cannot be read, and reads again after the stream comes back", async () => {
    const { list, server } = setup();
    server.failWith(new ProductOwnerError("NETWORK_ERROR", "offline"));
    list.attach("p1");
    await settled();
    assert.equal(list.state.error, "offline");
    server.failWith(null);
    server.setStatus("resynced");
    await settled();
    assert.equal(list.state.error, "");
    assert.deepEqual(ids(list), ["new", "old"]);
  });

  test("stops following when detached", () => {
    const { list, server } = setup();
    const stop = list.attach("p1");
    assert.equal(server.followers, 1);
    stop();
    assert.equal(server.followers, 0);
  });
});
