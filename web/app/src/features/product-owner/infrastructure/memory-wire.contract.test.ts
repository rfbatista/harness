// The memory part of the contract on the wire (Contract: product owner, Memory; ADR 0106):
// the bodies the HTTP adapter sends, and malformed answers, events and notes read as the port's error.

import { test } from "node:test";
import assert from "node:assert/strict";
import { apiClient } from "../../../shared/infrastructure/api-client";
import { memoryFeed } from "../../../shared/infrastructure/memory-feed";
import { ProductOwnerError } from "../application/ports";
import { fakeProductOwnerServer } from "../testing/fake-server";
import { entry } from "../testing/fixtures";
import { entryWire } from "../testing/wire";
import { eventsPath, httpProductOwner } from "./http-product-owner";
import { memoryProductOwner } from "./memory-product-owner";

function http() {
  const server = memoryProductOwner();
  const fake = fakeProductOwnerServer(server);
  const feed = memoryFeed();
  return { server, fake, feed, gateway: httpProductOwner(apiClient(fake.fetch), feed) };
}

const badResponse = (field: string) => (err: unknown) => err instanceof ProductOwnerError && err.code === "BAD_RESPONSE" && err.message.includes(field);

test("the HTTP adapter reads and changes memory with the contract's paths and wire field names", async () => {
  const { server, fake, gateway } = http();
  server.seedProject("p 1", true);
  server.seedMemory("p 1", [entry({ entryId: "e1", text: "Old" })]);
  await gateway.memory("p 1");
  await gateway.updateMemoryEntry("e1", "New");
  await gateway.deleteMemoryEntry("e1");
  assert.deepEqual(fake.requests.map((r) => [r.method, r.url, r.body]), [
    ["GET", "/api/product_owner_memory?project_id=p+1", undefined],
    ["POST", "/api/update_memory_entry", { entry_id: "e1", text: "New" }],
    ["POST", "/api/delete_memory_entry", { entry_id: "e1" }],
  ]);
});

test("a memory answer that does not match the contract is a BAD_RESPONSE naming the field", async () => {
  const { server, fake, gateway } = http();
  server.seedProject("p1", true);
  const good = entryWire(entry({ entryId: "e1" })) as Record<string, unknown>;
  fake.answerWith({ entries: [{ ...good, saved_at: undefined }] });
  await assert.rejects(gateway.memory("p1"), badResponse("saved_at"));
  fake.answerWith({ entries: [{ ...good, changed_by: "someone" }] });
  await assert.rejects(gateway.memory("p1"), badResponse("changed_by"));
  fake.answerWith({ entries: [{ ...good, text: 7 }] });
  await assert.rejects(gateway.memory("p1"), badResponse("text"));
  fake.answerWith({ entries: "none" });
  await assert.rejects(gateway.memory("p1"), badResponse("entries"));
  fake.answerWith({ ...good, entry_id: null });
  await assert.rejects(gateway.updateMemoryEntry("e1", "x"), badResponse("entry_id"));
});

test("a memory_note item without its op or entry is a BAD_RESPONSE, not a neutral line", async () => {
  const { server, fake, gateway } = http();
  server.seedProject("p1", true);
  server.seedThread("p1", { threadId: "t1" });
  const head = { thread_id: "t1", project_id: "p1", title: "x", turn: "idle", session_id: "s" };
  const note = { item_id: "n1", kind: "memory_note", at: "2026-11-24T10:00:00Z", op: "saved", entry_id: "e1", text: "Ship on Fridays." };
  fake.answerWith({ thread: head, items: [{ ...note, op: "forgotten" }], has_earlier: false });
  await assert.rejects(gateway.thread("t1"), badResponse("op"));
  fake.answerWith({ thread: head, items: [{ ...note, entry_id: undefined }], has_earlier: false });
  await assert.rejects(gateway.thread("t1"), badResponse("entry_id"));
  fake.answerWith({ thread: head, items: [note], has_earlier: false });
  assert.deepEqual((await gateway.thread("t1")).items, [{ kind: "memory_note", itemId: "n1", at: new Date("2026-11-24T10:00:00Z"), op: "saved", entryId: "e1", text: "Ship on Fridays." }]);
});

test("a product_owner_memory event is read with its wire names, and a malformed one is dropped", () => {
  const { feed, gateway } = http();
  const seen: unknown[] = [];
  const stop = gateway.follow("p1", (e) => seen.push(e), () => {});
  const path = eventsPath("p1");
  feed.emit(path, { product_owner_memory: { project_id: "p1", entry_id: "e1", op: "remembered" } });
  feed.emit(path, { product_owner_memory: { project_id: "p1", op: "saved" } });
  feed.emit(path, { product_owner_memory: { project_id: "p2", entry_id: "e9", op: "saved" } });
  feed.emit(path, { product_owner_memory: { project_id: "p1", entry_id: "e1", op: "dropped" } });
  assert.deepEqual(seen, [{ type: "memory", entryId: "e1", op: "dropped" }]);
  stop();
});
