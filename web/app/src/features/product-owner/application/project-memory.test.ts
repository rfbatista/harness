import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { memoryProductOwner } from "../infrastructure/memory-product-owner";
import { ProductOwnerError } from "./ports";
import { entry } from "../testing/fixtures";
import { ProjectMemory } from "./project-memory";

const settled = () => new Promise((resolve) => setImmediate(resolve));

const setup = async (entries = [entry({ entryId: "a", text: "Alpha", savedAt: new Date("2026-11-20T10:00:00Z") }), entry({ entryId: "b", text: "Beta", savedAt: new Date("2026-11-22T10:00:00Z") })]) => {
  const server = memoryProductOwner();
  server.seedProject("p1", true);
  server.seedProject("p2", true);
  server.seedMemory("p1", entries);
  server.seedMemory("p2", [entry({ entryId: "z", text: "Other project" })]);
  const memory = new ProjectMemory({ gateway: server, events: server });
  return { server, memory };
};

describe("the project's memory", () => {
  test("reads nothing until it is attached, then holds the entries newest first", async () => {
    const { server, memory } = await setup();
    assert.equal(memory.state.loaded, false);
    memory.attach("p1");
    assert.equal(memory.state.loading, true);
    await settled();
    assert.deepEqual(memory.state.entries.map((e) => e.entryId), ["b", "a"]);
    assert.equal(memory.state.loaded, true);
    assert.equal(server.followers, 1);
  });

  test("a saved, a changed and a dropped event each read the list again", async () => {
    const { server, memory } = await setup();
    memory.attach("p1");
    await settled();
    server.remember("p1", entry({ entryId: "c", text: "Gamma", savedAt: new Date("2026-11-23T10:00:00Z") }), "saved");
    await settled();
    assert.deepEqual(memory.state.entries.map((e) => e.entryId), ["c", "b", "a"]);
    server.remember("p1", entry({ entryId: "a", text: "Alpha, fixed", savedAt: new Date("2026-11-20T10:00:00Z"), changedAt: new Date("2026-11-23T11:00:00Z"), changedBy: "product_owner" }), "changed");
    await settled();
    assert.equal(memory.state.entries.find((e) => e.entryId === "a")!.text, "Alpha, fixed");
    server.forget("b");
    await settled();
    assert.deepEqual(memory.state.entries.map((e) => e.entryId), ["c", "a"]);
  });

  test("an entry that arrives after the first read is fresh; the first read is not", async () => {
    const { server, memory } = await setup();
    memory.attach("p1");
    await settled();
    assert.equal(memory.state.fresh.size, 0);
    server.remember("p1", entry({ entryId: "c", savedAt: new Date("2026-11-23T10:00:00Z") }), "saved");
    await settled();
    assert.deepEqual([...memory.state.fresh], ["c"]);
    server.remember("p1", entry({ entryId: "a", text: "Edited", savedAt: new Date("2026-11-20T10:00:00Z"), changedAt: new Date(), changedBy: "person" }), "changed");
    await settled();
    assert.deepEqual([...memory.state.fresh].sort(), ["a", "c"]);
  });

  test("an event of another project is ignored", async () => {
    const { server, memory } = await setup();
    memory.attach("p1");
    await settled();
    server.remember("p2", entry({ entryId: "y" }), "saved");
    await settled();
    assert.deepEqual(memory.state.entries.map((e) => e.entryId), ["b", "a"]);
  });

  test("a failed read says so and a retry reads again", async () => {
    const { server, memory } = await setup();
    server.failWith(new ProductOwnerError("NETWORK_ERROR", "offline"));
    memory.attach("p1");
    await settled();
    assert.equal(memory.state.error, "offline");
    assert.equal(memory.state.loading, false);
    server.failWith(null);
    memory.retry();
    await settled();
    assert.equal(memory.state.error, "");
    assert.equal(memory.state.entries.length, 2);
  });

  test("a read again after the stream resynced", async () => {
    const { server, memory } = await setup();
    memory.attach("p1");
    await settled();
    server.seedMemory("p1", [entry({ entryId: "q", savedAt: new Date("2026-11-24T10:00:00Z") })]);
    server.setStatus("resynced");
    await settled();
    assert.deepEqual(memory.state.entries.map((e) => e.entryId), ["q"]);
  });

  test("letting go stops following", async () => {
    const { server, memory } = await setup();
    const stop = memory.attach("p1");
    stop();
    assert.equal(server.followers, 0);
  });

  test("a person's edit changes the entry and a delete removes it, leaving a receipt", async () => {
    const { server, memory } = await setup();
    memory.attach("p1");
    await settled();
    await memory.update("a", "Alpha, edited");
    const a = memory.state.entries.find((e) => e.entryId === "a")!;
    assert.equal(a.text, "Alpha, edited");
    assert.equal(a.changedBy, "person");
    await memory.remove("b");
    assert.deepEqual(memory.state.entries.map((e) => e.entryId), ["a"]);
    assert.deepEqual(memory.state.receipts.map((r) => [r.entryId, r.text]), [["b", "Beta"]]);
    await settled();
    assert.deepEqual(memory.state.entries.map((e) => e.entryId), ["a"]);
    assert.equal(server.followers, 1);
  });

  test("an edit the server refuses throws its error and an entry that is gone leaves the list", async () => {
    const { server, memory } = await setup();
    memory.attach("p1");
    await settled();
    await assert.rejects(memory.update("a", "   "), (err: unknown) => err instanceof ProductOwnerError && err.code === "INVALID_INPUT");
    server.forget("a");
    await assert.rejects(memory.update("a", "x"), (err: unknown) => err instanceof ProductOwnerError && err.code === "MEMORY_ENTRY_NOT_FOUND");
    await settled();
    assert.deepEqual(memory.state.entries.map((e) => e.entryId), ["b"]);
  });
});
