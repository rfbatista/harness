// Gaps the review of the Memory tab found (Review: agent/project-product-owner-c62995bd).
// The tests marked todo state what the spec asks and the branch does not do yet.

import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { memoryAddress, memoryHref } from "../../application/links";
import { OwnerDock } from "../../application/owner-dock";
import { Conversation } from "../../application/conversation";
import { ProjectMemory } from "../../application/project-memory";
import { ThreadList } from "../../application/thread-list";
import type { ProductOwnerGateway } from "../../application/ports";
import type { MemoryEntry } from "../../domain/thread";
import { memoryProductOwner } from "../../infrastructure/memory-product-owner";
import { memoryDockMemory, memoryNavigator, memoryScreen } from "../../testing/memory-dock-adapters";
import { entry, memoryNote } from "../../testing/fixtures";
import { AboutPresenter } from "./about-presenter";
import { MemoryTabPresenter } from "./memory-tab-presenter";
import { PanelPresenter } from "./panel-presenter";
import { ThreadListPresenter } from "./thread-list-presenter";
import { ThreadPresenter } from "./thread-presenter";

const settled = () => new Promise((resolve) => setImmediate(resolve));

describe("the memory address of a page", () => {
  test("round-trips an entry id that needs escaping", () => {
    const href = memoryHref("p1", "a/b c#d");
    assert.equal(href, "/projects/p1/product-owner#memory/a%2Fb%20c%23d");
    assert.deepEqual(memoryAddress(href.slice(href.indexOf("#"))), { tab: "memory", entryId: "a/b c#d" });
    assert.equal(memoryAddress("#memoryx"), undefined);
    assert.equal(memoryAddress("#threads"), undefined);
  });

  test("a malformed escape in the address does not throw, so the page island still mounts", { todo: "memoryAddress calls decodeURIComponent unguarded: #memory/% throws URIError and the owner-page island is not mounted" }, () => {
    assert.doesNotThrow(() => memoryAddress("#memory/%E0%A4%A"));
    assert.equal(memoryAddress("#memory/%")?.tab, "memory");
  });
});

describe("the panel's memory", () => {
  test("is followed while the panel is docked, and let go when it closes and when it is destroyed", async () => {
    const server = memoryProductOwner();
    server.seedProject("p1", true);
    server.seedMemory("p1", [entry({ entryId: "a" })]);
    const dock = new OwnerDock({ gateway: server, events: server, memory: memoryDockMemory(), screen: memoryScreen(), navigator: memoryNavigator() });
    const conversation = new Conversation({ gateway: server, events: server, onThread: (id) => dock.setThreadId(id) });
    const list = new ThreadList({ gateway: server, events: server });
    const memory = new ProjectMemory({ gateway: server, events: server });
    const thread = new ThreadPresenter(conversation, { projectName: "harness", noTasks: false }, { count: () => 0, find: () => null }, false, { memory });
    const panel = new PanelPresenter(dock, conversation, thread, { memory, list, threads: new ThreadListPresenter(list, conversation, { now: () => Date.now() }), about: new AboutPresenter(conversation, list) });
    dock.bind("p1");
    assert.equal(server.followers, 1, "the dock's own follower only, and no memory read, while the panel is closed");
    assert.equal(memory.state.loaded, false);
    dock.open();
    await settled();
    assert.equal(server.followers, 4, "the dock, the conversation, the thread list and the memory");
    assert.equal(thread.viewModel.empty?.remembered, "I remember 1 thing we agreed");
    panel.close();
    assert.equal(server.followers, 1);
    dock.open();
    await settled();
    panel.destroy();
    assert.equal(server.followers, 1);
  });
});

describe("the in-thread note's Edit link", () => {
  test("goes to the page's Memory tab at the entry, escaped, from the panel", async () => {
    const server = memoryProductOwner();
    server.seedProject("p1", true);
    server.seedMemory("p1", [entry({ entryId: "e 1" })]);
    server.seedThread("p1", { threadId: "t1" }, [memoryNote({ itemId: "n1", entryId: "e 1" })]);
    const conversation = new Conversation({ gateway: server, events: server });
    const memory = new ProjectMemory({ gateway: server, events: server });
    const thread = new ThreadPresenter(conversation, { projectName: "harness", noTasks: false }, { count: () => 0, find: () => null }, false, { memory });
    memory.attach("p1");
    conversation.attach("p1", "t1");
    await settled();
    const note = thread.viewModel.rows.find((r) => r.kind === "memory_note");
    assert.equal(note?.kind === "memory_note" ? note.editHref : undefined, "/projects/p1/product-owner#memory/e%201");
    assert.equal(thread.hasMemoryTab, false);
  });
});

describe("the Memory tab under concurrency", () => {
  test("a second click on Delete entry while the first is under way leaves one receipt", { todo: "confirmDelete has no in-flight guard: two deletes run and two receipts with the same React key stand" }, async () => {
    const server = memoryProductOwner();
    server.seedProject("p1", true);
    server.seedMemory("p1", [entry({ entryId: "a", text: "Alpha" }), entry({ entryId: "b", text: "Beta" })]);
    const memory = new ProjectMemory({ gateway: server, events: server });
    const presenter = new MemoryTabPresenter(memory, "p1");
    memory.attach("p1");
    await settled();
    presenter.askDelete("a");
    await Promise.all([presenter.confirmDelete(), presenter.confirmDelete()]);
    await settled();
    const receipts = presenter.viewModel.rows.filter((r) => r.kind === "receipt");
    assert.equal(receipts.length, 1);
    assert.equal(new Set(presenter.viewModel.rows.map((r) => r.key)).size, presenter.viewModel.rows.length, "every row key is unique");
  });

  test("an older read that answers after a newer one does not bring back a dropped entry", { todo: "ProjectMemory.#read applies whichever read answers last; reads are not ordered" }, async () => {
    const server = memoryProductOwner();
    server.seedProject("p1", true);
    const a = entry({ entryId: "a", text: "Alpha" });
    server.seedMemory("p1", [a]);
    // The first two reads answer when the test lets them, the newer first.
    const held: Array<{ entries: readonly MemoryEntry[]; release: () => void }> = [];
    const gateway: ProductOwnerGateway = {
      ...server,
      memory: async (projectId) => {
        const entries = await server.memory(projectId);
        if (held.length >= 2) return entries;
        let release = () => {};
        const wait = new Promise<void>((resolve) => (release = resolve));
        held.push({ entries, release });
        await wait;
        return entries;
      },
    };
    const memory = new ProjectMemory({ gateway, events: server });
    memory.attach("p1"); // read 1: sees "a"
    await settled();
    server.forget("a"); // read 2: sees nothing
    await settled();
    assert.equal(held.length, 2);
    held[1]!.release();
    await settled();
    held[0]!.release();
    await settled();
    assert.deepEqual(memory.state.entries.map((e) => e.entryId), []);
  });
});
