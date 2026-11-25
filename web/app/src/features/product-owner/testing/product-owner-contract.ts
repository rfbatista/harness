// The contract every product owner adapter keeps (Contract: Harness server ↔ Web UI —
// product owner). Run against the HTTP adapter over a fake server and the in-memory adapter.

import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { ProductOwnerError, type ProductOwnerEvents, type ProductOwnerGateway } from "../application/ports";
import type { ProductOwnerEvent } from "../domain/thread";
import type { MemoryProductOwner } from "../infrastructure/memory-product-owner";
import { entry, failure, memoryNote, message, person, summary } from "./fixtures";

export interface ProductOwnerHarness {
  /** The server's side: the harness data and the stream, seen through the adapter under test. */
  server: MemoryProductOwner;
  gateway: ProductOwnerGateway;
  events: ProductOwnerEvents;
}

const rejectsWith = (code: string) => (err: unknown) => err instanceof ProductOwnerError && err.code === code;

export function productOwnerContract(name: string, make: () => ProductOwnerHarness): void {
  describe(`ProductOwner contract: ${name}`, () => {
    const setup = () => {
      const h = make();
      h.server.seedProject("p1", true);
      h.server.seedProject("p2", false);
      return h;
    };

    test("reads and changes a project's switch", async () => {
      const h = setup();
      assert.deepEqual(await h.gateway.settings("p1"), { projectId: "p1", enabled: true });
      assert.deepEqual(await h.gateway.setEnabled("p1", false), { projectId: "p1", enabled: false });
      assert.equal((await h.gateway.settings("p1")).enabled, false);
      await assert.rejects(h.gateway.settings("nope"), rejectsWith("PROJECT_NOT_FOUND"));
    });

    test("lists a project's threads, the most recently active first", async () => {
      const h = setup();
      const old = summary({ threadId: "old", lastActivityAt: new Date("2026-11-20T10:00:00Z") });
      const fresh = summary({ threadId: "fresh", lastActivityAt: new Date("2026-11-23T10:00:00Z"), titleSetByPerson: true, turn: "running", pendingHeldActions: 2 });
      h.server.seedThread("p1", old);
      h.server.seedThread("p1", fresh);
      h.server.seedThread("p2", summary({ threadId: "other" }));
      const threads = await h.gateway.threads("p1");
      assert.deepEqual(threads.map((t) => t.threadId), ["fresh", "old"]);
      assert.deepEqual(threads[0], { ...fresh });
    });

    test("pages a thread, oldest first, 100 items at a time", async () => {
      const h = setup();
      const items = Array.from({ length: 130 }, (_, i) => message({ itemId: `x${String(i).padStart(3, "0")}`, text: `n${i}` }));
      h.server.seedThread("p1", { threadId: "t1", title: "Long" }, items, "sess-1");
      const latest = await h.gateway.thread("t1");
      assert.equal(latest.items.length, 100);
      assert.equal(latest.items[0]!.itemId, "x030");
      assert.equal(latest.hasEarlier, true);
      assert.deepEqual(latest.head, { threadId: "t1", projectId: "p1", title: "Long", turn: "idle", sessionId: "sess-1" });
      const earlier = await h.gateway.thread("t1", "x030");
      assert.deepEqual(earlier.items.map((i) => i.itemId), items.slice(0, 30).map((i) => i.itemId));
      assert.equal(earlier.hasEarlier, false);
      await assert.rejects(h.gateway.thread("nope"), rejectsWith("THREAD_NOT_FOUND"));
    });

    test("reads every kind of item, and shows the ones this piece does not draw as neutral lines", async () => {
      const h = setup();
      const at = new Date("2026-11-23T10:00:00Z");
      const items = [
        person("hello"),
        { kind: "activity" as const, itemId: "a1", at, text: "Reading the board" },
        message({ itemId: "m9", text: "partial", streaming: true, stopped: true }),
        { kind: "notice" as const, itemId: "n1", at, text: "Continued after a restart" },
        failure({ itemId: "f1", retryItemId: "m9" }),
        { kind: "other" as const, wireKind: "held_action", itemId: "h1", at, text: "Archive task X" },
        { kind: "other" as const, wireKind: "checkpoint", itemId: "c1", at, text: "Summary so far" },
        memoryNote({ itemId: "mn1", op: "changed", entryId: "e9", text: "Digests at 09:00" }),
      ];
      h.server.seedThread("p1", { threadId: "t1" }, items);
      assert.deepEqual((await h.gateway.thread("t1")).items, items);
    });

    test("starts a thread, answers the ids, and shows the person's message in the thread", async () => {
      const h = setup();
      const started = await h.gateway.startThread("p1", "What was done?");
      const page = await h.gateway.thread(started.threadId);
      assert.equal(page.items[0]!.itemId, started.itemId);
      assert.equal(page.items[0]!.kind === "message" && page.items[0]!.text, "What was done?");
    });

    test("sends a message to a thread, and refuses while it is off, busy or invalid", async () => {
      const h = setup();
      h.server.seedThread("p1", { threadId: "t1" });
      h.server.seedThread("p2", { threadId: "t2" });
      const sent = await h.gateway.sendMessage("t1", "More?");
      assert.ok(sent.itemId);
      await assert.rejects(h.gateway.sendMessage("t1", "again"), rejectsWith("THREAD_BUSY"));
      h.server.setTurn("t1", "idle");
      await assert.rejects(h.gateway.sendMessage("t1", "  "), rejectsWith("INVALID_INPUT"));
      await assert.rejects(h.gateway.sendMessage("t2", "hi"), rejectsWith("PRODUCT_OWNER_OFF"));
      await assert.rejects(h.gateway.startThread("p2", "hi"), rejectsWith("PRODUCT_OWNER_OFF"));
      await assert.rejects(h.gateway.sendMessage("nope", "hi"), rejectsWith("THREAD_NOT_FOUND"));
    });

    test("carries the fix link of PRODUCT_OWNER_UNAVAILABLE", async () => {
      const h = setup();
      h.server.failWith(new ProductOwnerError("PRODUCT_OWNER_UNAVAILABLE", "claude cannot run", "/server"));
      await assert.rejects(h.gateway.startThread("p1", "hi"), (err: unknown) => err instanceof ProductOwnerError && err.code === "PRODUCT_OWNER_UNAVAILABLE" && err.fixUrl === "/server");
    });

    test("stops a turn", async () => {
      const h = setup();
      h.server.seedThread("p1", { threadId: "t1", turn: "running" });
      await h.gateway.stopTurn("t1");
      assert.deepEqual(h.server.stops, ["t1"]);
    });

    test("renames a thread as the person's, and refuses an empty or long title", async () => {
      const h = setup();
      h.server.seedThread("p1", { threadId: "t1" });
      const renamed = await h.gateway.renameThread("t1", "Roadmap");
      assert.equal(renamed.title, "Roadmap");
      assert.equal(renamed.titleSetByPerson, true);
      await assert.rejects(h.gateway.renameThread("t1", ""), rejectsWith("INVALID_INPUT"));
      await assert.rejects(h.gateway.renameThread("t1", "x".repeat(201)), rejectsWith("INVALID_INPUT"));
    });

    test("reads a project's memory newest first, never another project's", async () => {
      const h = setup();
      const old = entry({ entryId: "old", savedAt: new Date("2026-11-20T10:00:00Z") });
      const changed = entry({ entryId: "new", savedAt: new Date("2026-11-23T10:00:00Z"), changedAt: new Date("2026-11-24T08:00:00Z"), changedBy: "person", threadId: "t9", threadTitle: "Shaping the digest" });
      h.server.seedMemory("p1", [old, changed]);
      h.server.seedMemory("p2", [entry({ entryId: "other" })]);
      assert.deepEqual(await h.gateway.memory("p1"), [changed, old]);
      assert.deepEqual(await h.gateway.memory("nope").catch((e: unknown) => (e instanceof ProductOwnerError ? e.code : "?")), "PROJECT_NOT_FOUND");
    });

    test("edits and deletes a memory entry, and refuses an empty, a long or an unknown one", async () => {
      const h = setup();
      h.server.seedMemory("p1", [entry({ entryId: "e1", text: "Old" })]);
      const edited = await h.gateway.updateMemoryEntry("e1", "New text");
      assert.equal(edited.text, "New text");
      assert.equal(edited.changedBy, "person");
      assert.ok(edited.changedAt);
      assert.equal((await h.gateway.memory("p1"))[0]!.text, "New text");
      await assert.rejects(h.gateway.updateMemoryEntry("e1", " "), rejectsWith("INVALID_INPUT"));
      await assert.rejects(h.gateway.updateMemoryEntry("e1", "x".repeat(4001)), rejectsWith("INVALID_INPUT"));
      // 4,000 code points that are 8,000 UTF-16 units are accepted.
      assert.equal((await h.gateway.updateMemoryEntry("e1", "😀".repeat(4000))).text.length, 8000);
      await h.gateway.deleteMemoryEntry("e1");
      assert.deepEqual(await h.gateway.memory("p1"), []);
      await assert.rejects(h.gateway.deleteMemoryEntry("e1"), rejectsWith("MEMORY_ENTRY_NOT_FOUND"));
      await assert.rejects(h.gateway.updateMemoryEntry("nope", "x"), rejectsWith("MEMORY_ENTRY_NOT_FOUND"));
    });

    test("tells the project's followers when memory is saved, changed and dropped", async () => {
      const h = setup();
      const seen: ProductOwnerEvent[] = [];
      h.events.follow("p1", (e) => seen.push(e), () => {});
      h.server.remember("p1", entry({ entryId: "e1" }), "saved");
      await h.gateway.updateMemoryEntry("e1", "Edited");
      await h.gateway.deleteMemoryEntry("e1");
      h.server.remember("p2", entry({ entryId: "e2" }), "saved");
      assert.deepEqual(seen, [
        { type: "memory", entryId: "e1", op: "saved" },
        { type: "memory", entryId: "e1", op: "changed" },
        { type: "memory", entryId: "e1", op: "dropped" },
      ]);
    });

    test("follows its project's events, and not another project's", () => {
      const h = setup();
      const seen: ProductOwnerEvent[] = [];
      const stop = h.events.follow("p1", (e) => seen.push(e), () => {});
      const item = message({ itemId: "m1" });
      h.server.seedThread("p1", { threadId: "t1" });
      h.server.seedThread("p2", { threadId: "t2" });
      h.server.publish("p1", { type: "item", threadId: "t1", item });
      h.server.publish("p2", { type: "delta", threadId: "t2", itemId: "m1", text: "no" });
      h.server.publish("p1", { type: "delta", threadId: "t1", itemId: "m1", text: " more" });
      h.server.publish("p1", { type: "settings", settings: { projectId: "p1", enabled: false } });
      assert.deepEqual(seen.map((e) => e.type), ["item", "delta", "settings"]);
      assert.deepEqual(seen[1], { type: "delta", threadId: "t1", itemId: "m1", text: " more" });
      stop();
      h.server.publish("p1", { type: "settings", settings: { projectId: "p1", enabled: true } });
      assert.equal(seen.length, 3);
    });

    test("tells when the stream drops and comes back", () => {
      const h = setup();
      const states: string[] = [];
      const stop = h.events.follow("p1", () => {}, (s) => states.push(s));
      h.server.setStatus("paused");
      h.server.setStatus("resynced");
      stop();
      assert.deepEqual(states.slice(-2), ["paused", "resynced"]);
    });
  });
}
