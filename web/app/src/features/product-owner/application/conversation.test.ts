import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { memoryProductOwner } from "../infrastructure/memory-product-owner";
import { failure, message, person } from "../testing/fixtures";
import { Conversation } from "./conversation";
import { ProductOwnerError } from "./ports";

const settled = () => new Promise((resolve) => setImmediate(resolve));

const setup = (opts: { enabled?: boolean } = {}) => {
  const server = memoryProductOwner();
  server.seedProject("p1", opts.enabled ?? true);
  const threadIds: (string | null)[] = [];
  const conversation = new Conversation({ gateway: server, events: server, onThread: (id) => threadIds.push(id) });
  return { server, conversation, threadIds };
};
const texts = (c: Conversation) => c.state.items.map((i) => (i.kind === "message" ? `${i.author}:${i.text}${i.sending ? "…" : ""}` : i.kind));

describe("a fresh conversation", () => {
  test("has no thread and no items, and says it is fresh", () => {
    const { conversation } = setup();
    conversation.attach("p1", null);
    assert.equal(conversation.state.threadId, null);
    assert.deepEqual(conversation.state.items, []);
    assert.equal(conversation.state.fresh, true);
  });

  test("the first message starts a thread; the person's message shows at once as sending, then as sent", async () => {
    const { conversation, server, threadIds } = setup();
    conversation.attach("p1", null);
    server.silenceEchoes(true);
    const release = server.holdNextSend();
    const sending = conversation.send("What was done?");
    assert.deepEqual(texts(conversation), ["person:What was done?…"]);
    assert.equal(conversation.state.fresh, false);
    release();
    await sending;
    assert.deepEqual(texts(conversation), ["person:What was done?"]);
    assert.equal(conversation.state.threadId, threadIds.at(-1));
    assert.ok(conversation.state.threadId);
  });

  test("the echo of the person's own message before the answer shows once", async () => {
    const { conversation, server } = setup();
    conversation.attach("p1", null);
    // The memory server publishes the echo inside startThread, before the call answers: the echo wins the race.
    await conversation.send("hello");
    assert.deepEqual(texts(conversation), ["person:hello"]);
    assert.equal(server.itemsOf(conversation.state.threadId!).length, 1);
  });

  test("the echo after the answer shows once too", async () => {
    const { conversation, server } = setup();
    server.seedThread("p1", { threadId: "t1" });
    conversation.attach("p1", "t1");
    await settled();
    server.silenceEchoes(true);
    const id = (await conversation.send("hello"), conversation.state.items[0]!.itemId);
    server.silenceEchoes(false);
    server.publish("p1", { type: "item", threadId: "t1", item: person("hello", { itemId: id }) });
    assert.deepEqual(texts(conversation), ["person:hello"]);
  });
});

describe("an answer", () => {
  const start = async () => {
    const h = setup();
    h.server.seedThread("p1", { threadId: "t1" }, [person("Q")]);
    h.conversation.attach("p1", "t1");
    await settled();
    return h;
  };

  test("streams in as deltas and is replaced by the final item", async () => {
    const { conversation, server } = await start();
    server.publish("p1", { type: "thread", thread: { ...(await server.threads("p1"))[0]!, turn: "running" } });
    server.publish("p1", { type: "item", threadId: "t1", item: message({ itemId: "a1", text: "", streaming: true }) });
    server.publish("p1", { type: "delta", threadId: "t1", itemId: "a1", text: "Child 1 " });
    server.publish("p1", { type: "delta", threadId: "t1", itemId: "a1", text: "is in review" });
    const streaming = conversation.state.items.at(-1)!;
    assert.deepEqual(streaming.kind === "message" && [streaming.text, streaming.streaming], ["Child 1 is in review", true]);
    server.publish("p1", { type: "item", threadId: "t1", item: message({ itemId: "a1", text: "Child 1 is in review.", streaming: false }) });
    const done = conversation.state.items.at(-1)!;
    assert.deepEqual(done.kind === "message" && [done.text, done.streaming], ["Child 1 is in review.", false]);
    assert.equal(conversation.state.items.length, 2);
  });

  test("a delta for an item not seen yet starts a streaming answer", async () => {
    const { conversation, server } = await start();
    server.publish("p1", { type: "delta", threadId: "t1", itemId: "a9", text: "Hi" });
    assert.deepEqual(texts(conversation), ["person:Q", "product_owner:Hi"]);
  });

  test("a stopped answer stays in the thread, marked stopped", async () => {
    const { conversation, server } = await start();
    server.publish("p1", { type: "item", threadId: "t1", item: message({ itemId: "a1", text: "Partial", streaming: false, stopped: true }) });
    const item = conversation.state.items.at(-1)!;
    assert.equal(item.kind === "message" && item.stopped, true);
  });

  test("events of another thread are not shown", async () => {
    const { conversation, server } = await start();
    server.seedThread("p1", { threadId: "t2" });
    server.publish("p1", { type: "item", threadId: "t2", item: message({ itemId: "z", text: "other" }) });
    assert.deepEqual(texts(conversation), ["person:Q"]);
  });

  test("the turn follows the thread events, and stop asks the server", async () => {
    const { conversation, server } = await start();
    assert.equal(conversation.state.turn, "idle");
    server.setTurn("t1", "running");
    assert.equal(conversation.state.turn, "running");
    await conversation.stop();
    assert.deepEqual(server.stops, ["t1"]);
    server.setTurn("t1", "idle");
    assert.equal(conversation.state.turn, "idle");
  });
});

describe("a refusal", () => {
  test("keeps the typed text, drops the sending message and says why", async () => {
    const { conversation, server } = setup();
    server.seedThread("p1", { threadId: "t1", turn: "running" });
    conversation.attach("p1", "t1");
    await settled();
    conversation.setDraft("more");
    await conversation.send();
    assert.equal(conversation.state.draft, "more");
    assert.deepEqual(conversation.state.items, []);
    assert.equal(conversation.state.error?.code, "THREAD_BUSY");
  });

  test("PRODUCT_OWNER_UNAVAILABLE carries its fix link", async () => {
    const { conversation, server } = setup();
    conversation.attach("p1", null);
    server.failWith(new ProductOwnerError("PRODUCT_OWNER_UNAVAILABLE", "claude cannot run", "/server"));
    await conversation.send("hi");
    assert.equal(conversation.state.error?.fixUrl, "/server");
    assert.equal(conversation.state.draft, "hi");
  });

  test("an empty message is not sent", async () => {
    const { conversation, server } = setup();
    conversation.attach("p1", null);
    await conversation.send("   ");
    assert.equal(server.stops.length, 0);
    assert.deepEqual(conversation.state.items, []);
    assert.equal(conversation.state.error, null);
  });
});

describe("reading a thread", () => {
  test("loads its items and its turn, and remembers the thread", async () => {
    const { conversation, server, threadIds } = setup();
    server.seedThread("p1", { threadId: "t1", title: "Roadmap", turn: "running" }, [person("Q"), failure()]);
    conversation.attach("p1", "t1");
    await settled();
    assert.deepEqual(texts(conversation), ["person:Q", "failure"]);
    assert.equal(conversation.state.title, "Roadmap");
    assert.equal(conversation.state.turn, "running");
    assert.equal(conversation.state.fresh, false);
    assert.deepEqual(threadIds, ["t1"]);
  });

  test("a thread that is gone starts a fresh conversation", async () => {
    const { conversation, threadIds } = setup();
    conversation.attach("p1", "gone");
    await settled();
    assert.equal(conversation.state.threadId, null);
    assert.equal(conversation.state.fresh, true);
    assert.equal(threadIds.at(-1), null);
  });

  test("detaching stops following", async () => {
    const { conversation, server } = setup();
    const detach = conversation.attach("p1", null);
    assert.equal(server.followers, 1);
    detach();
    assert.equal(server.followers, 0);
  });

  test("a new thread clears the conversation", async () => {
    const { conversation, server, threadIds } = setup();
    server.seedThread("p1", { threadId: "t1" }, [person("Q")]);
    conversation.attach("p1", "t1");
    await settled();
    conversation.newThread();
    assert.equal(conversation.state.threadId, null);
    assert.deepEqual(conversation.state.items, []);
    assert.equal(threadIds.at(-1), null);
  });
});

describe("two tabs on one thread", () => {
  const twoTabs = async () => {
    const server = memoryProductOwner();
    server.seedProject("p1", true);
    server.seedThread("p1", { threadId: "t1" }, [person("Q")]);
    const make = () => new Conversation({ gateway: server, events: server });
    const [a, b] = [make(), make()];
    a.attach("p1", "t1");
    b.attach("p1", "t1");
    await settled();
    return { server, a, b };
  };

  test("show the same messages, and the other tab's message arrives marked as from another tab", async () => {
    const { a, b } = await twoTabs();
    await a.send("From tab A");
    const mine = a.state.items.at(-1)!;
    const theirs = b.state.items.at(-1)!;
    assert.equal(mine.itemId, theirs.itemId);
    assert.equal(mine.kind === "message" && mine.fromOtherTab, undefined, "my own message is not from another tab");
    assert.equal(theirs.kind === "message" && theirs.fromOtherTab, true);
    assert.deepEqual(a.state.items.map((i) => i.itemId), b.state.items.map((i) => i.itemId));
  });

  test("see the same answer as it streams", async () => {
    const { server, a, b } = await twoTabs();
    server.publish("p1", { type: "delta", threadId: "t1", itemId: "a1", text: "Hello" });
    server.publish("p1", { type: "item", threadId: "t1", item: message({ itemId: "a1", text: "Hello there." }) });
    assert.deepEqual(texts(a), texts(b));
    assert.equal(texts(a).at(-1), "product_owner:Hello there.");
  });
});

describe("a stream that drops", () => {
  test("is read again when it comes back, and what was missed shows, without a doubled message", async () => {
    const { conversation, server } = setup();
    server.seedThread("p1", { threadId: "t1" }, [person("Q", { itemId: "m1" })]);
    conversation.attach("p1", "t1");
    await settled();
    server.setStatus("paused");
    // Missed while paused: the server holds an answer the stream never delivered.
    server.seedThread("p1", { threadId: "t1" }, [person("Q", { itemId: "m1" }), message({ itemId: "a1", text: "Missed answer" })]);
    server.setStatus("resynced");
    await settled();
    assert.deepEqual(texts(conversation), ["person:Q", "product_owner:Missed answer"]);
  });

  test("keeps the message that is still sending when it is read again", async () => {
    const { conversation, server } = setup();
    server.seedThread("p1", { threadId: "t1" });
    conversation.attach("p1", "t1");
    await settled();
    server.silenceEchoes(true);
    const release = server.holdNextSend();
    const sending = conversation.send("Hi");
    server.setStatus("resynced");
    await settled();
    assert.deepEqual(texts(conversation), ["person:Hi"], "the server already stored it: shown once, and no longer sending");
    release();
    await sending;
  });
});

describe("a restart", () => {
  test("shows its notice and keeps streaming", async () => {
    const { conversation, server } = setup();
    server.seedThread("p1", { threadId: "t1", turn: "interrupted" }, [person("Q")]);
    conversation.attach("p1", "t1");
    await settled();
    server.publish("p1", { type: "item", threadId: "t1", item: { kind: "notice", itemId: "n1", at: new Date(), text: "Continued after a restart" } });
    server.publish("p1", { type: "delta", threadId: "t1", itemId: "a1", text: "Back" });
    assert.deepEqual(texts(conversation), ["person:Q", "notice", "product_owner:Back"]);
    assert.equal(conversation.state.turn, "interrupted");
  });
});

describe("a long thread", () => {
  const long = async () => {
    const { conversation, server } = setup();
    const items = Array.from({ length: 230 }, (_, i) => message({ itemId: `x${String(i).padStart(3, "0")}`, text: `n${i}` }));
    server.seedThread("p1", { threadId: "t1" }, items);
    conversation.attach("p1", "t1");
    await settled();
    return { conversation, items };
  };

  test("loads the last hundred and pages back a hundred at a time, in order, without a repeat", async () => {
    const { conversation, items } = await long();
    assert.equal(conversation.state.items.length, 100);
    assert.equal(conversation.state.hasEarlier, true);
    await conversation.loadEarlier();
    assert.equal(conversation.state.items.length, 200);
    await conversation.loadEarlier();
    assert.equal(conversation.state.hasEarlier, false);
    assert.deepEqual(conversation.state.items.map((i) => i.itemId), items.map((i) => i.itemId));
  });

  test("a page that arrives twice adds nothing", async () => {
    const { conversation } = await long();
    await Promise.all([conversation.loadEarlier(), conversation.loadEarlier()]);
    assert.equal(conversation.state.items.length, 200);
  });
});
