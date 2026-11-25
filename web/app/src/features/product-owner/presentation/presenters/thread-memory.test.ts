import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { Conversation } from "../../application/conversation";
import { ProjectMemory } from "../../application/project-memory";
import { memoryProductOwner } from "../../infrastructure/memory-product-owner";
import { entry, memoryNote, message, person } from "../../testing/fixtures";
import { ThreadPresenter } from "./thread-presenter";

const settled = () => new Promise((resolve) => setImmediate(resolve));

const setup = async (entries = [entry({ entryId: "e1", text: "The release is planned for 20 Nov." })]) => {
  const server = memoryProductOwner();
  server.seedProject("p1", true);
  server.seedMemory("p1", entries);
  const conversation = new Conversation({ gateway: server, events: server });
  const memory = new ProjectMemory({ gateway: server, events: server });
  const opened: string[] = [];
  const presenter = new ThreadPresenter(conversation, { projectName: "harness", noTasks: false }, { count: () => 1, find: () => null }, false, { memory, openEntry: (id) => opened.push(id) });
  memory.attach("p1");
  await settled();
  return { server, conversation, memory, presenter, opened };
};

describe("memory notes in the thread", () => {
  test("draw a saved, a changed and a dropped note with the entry's text, in the place of the neutral line", async () => {
    const { server, conversation, presenter } = await setup();
    server.seedThread("p1", { threadId: "t1" }, [
      person("We decided"),
      message({ text: "Saved it." }),
      memoryNote({ itemId: "n1", op: "saved", entryId: "e1", text: "The release is planned for 20 Nov." }),
      memoryNote({ itemId: "n2", op: "changed", entryId: "e1", text: "The release moved to 27 Nov." }),
      memoryNote({ itemId: "n3", op: "dropped", entryId: "gone", text: "Old fact" }),
    ]);
    conversation.attach("p1", "t1");
    await settled();
    const notes = presenter.viewModel.rows.filter((r) => r.kind === "memory_note");
    assert.deepEqual(notes.map((n) => [n.op, n.label, n.text]), [
      ["saved", "Saved to memory", "The release is planned for 20 Nov."],
      ["changed", "Updated in memory", "The release moved to 27 Nov."],
      ["dropped", "Dropped from memory", "Old fact"],
    ]);
    assert.equal(presenter.viewModel.rows.some((r) => r.kind === "other"), false);
  });

  test("link to the entry in Memory when it is still there, and to none when it was dropped or is gone", async () => {
    const { server, conversation, presenter, opened } = await setup();
    server.seedThread("p1", { threadId: "t1" }, [
      memoryNote({ itemId: "n1", op: "saved", entryId: "e1" }),
      memoryNote({ itemId: "n2", op: "changed", entryId: "removed" }),
      memoryNote({ itemId: "n3", op: "dropped", entryId: "e1" }),
    ]);
    conversation.attach("p1", "t1");
    await settled();
    const notes = presenter.viewModel.rows.filter((r) => r.kind === "memory_note");
    assert.deepEqual(notes.map((n) => n.editHref), ["/projects/p1/product-owner#memory/e1", null, null]);
    presenter.openMemoryEntry("e1");
    assert.deepEqual(opened, ["e1"]);
  });

  test("have no link while the memory is not read", async () => {
    const { server, conversation, presenter, memory } = await setup();
    memory.attach("p2");
    server.seedThread("p1", { threadId: "t1" }, [memoryNote({ itemId: "n1", entryId: "e1" })]);
    conversation.attach("p1", "t1");
    await settled();
    const note = presenter.viewModel.rows.find((r) => r.kind === "memory_note")!;
    assert.equal(note.kind === "memory_note" && note.editHref, null);
  });

  test("a note that arrives live shows once, and is announced politely", async () => {
    const { server, conversation, presenter } = await setup();
    server.seedThread("p1", { threadId: "t1" }, [person("hi")]);
    conversation.attach("p1", "t1");
    await settled();
    assert.equal(presenter.viewModel.announcement, "");
    const note = memoryNote({ itemId: "n1", op: "saved", entryId: "e1" });
    server.publish("p1", { type: "item", threadId: "t1", item: note });
    server.publish("p1", { type: "item", threadId: "t1", item: note });
    assert.equal(presenter.viewModel.rows.filter((r) => r.kind === "memory_note").length, 1);
    assert.equal(presenter.viewModel.announcement, "Saved to memory");
  });

  test("render the entry's text as text, never as markup", async () => {
    const { server, conversation, presenter } = await setup();
    server.seedThread("p1", { threadId: "t1" }, [memoryNote({ itemId: "n1", text: "<b>bold</b>" })]);
    conversation.attach("p1", "t1");
    await settled();
    const note = presenter.viewModel.rows.find((r) => r.kind === "memory_note")!;
    assert.equal(note.kind === "memory_note" && note.text, "<b>bold</b>");
  });
});

describe("a fresh thread's intro", () => {
  test("counts what the product owner remembers", async () => {
    const { conversation, presenter, server } = await setup([entry({ entryId: "a" }), entry({ entryId: "b" })]);
    conversation.attach("p1", null);
    assert.equal(presenter.viewModel.empty?.remembered, "I remember 2 things we agreed");
    server.remember("p1", entry({ entryId: "c" }), "saved");
    await settled();
    assert.equal(presenter.viewModel.empty?.remembered, "I remember 3 things we agreed");
    server.forget("a");
    server.forget("b");
    await settled();
    assert.equal(presenter.viewModel.empty?.remembered, "I remember 1 thing we agreed");
  });

  test("says nothing when nothing is remembered, or the memory is not read", async () => {
    const { conversation, presenter, server, memory } = await setup([]);
    conversation.attach("p1", null);
    assert.equal(presenter.viewModel.empty?.remembered, "");
    server.seedMemory("p1", [entry()]);
    memory.attach("p9");
    assert.equal(presenter.viewModel.empty?.remembered, "");
  });
});
