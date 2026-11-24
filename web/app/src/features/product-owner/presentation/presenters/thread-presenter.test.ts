import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { Conversation } from "../../application/conversation";
import { memoryProductOwner } from "../../infrastructure/memory-product-owner";
import type { ThreadItem } from "../../domain/thread";
import { failure, message, person } from "../../testing/fixtures";
import { FIRST_FEATURE_STARTER, ThreadPresenter, STARTERS, OFF_PLACEHOLDER } from "./thread-presenter";

const settled = () => new Promise((resolve) => setImmediate(resolve));
const setup = (context = { projectName: "harness", noTasks: false }) => {
  const server = memoryProductOwner();
  server.seedProject("p1", true);
  const conversation = new Conversation({ gateway: server, events: server });
  const presenter = new ThreadPresenter(conversation, context, { count: () => 1, find: (id) => (id === "t9" ? { state: "waiting" } : null) });
  return { server, conversation, presenter };
};

describe("a fresh thread", () => {
  test("offers the four starters, and a click sends the starter's text", async () => {
    const { conversation, presenter, server } = setup();
    conversation.attach("p1", null);
    assert.deepEqual(presenter.viewModel.empty?.starters, STARTERS);
    assert.equal(presenter.viewModel.empty?.heading, "Ask about harness");
    await presenter.startWith(STARTERS[0]);
    assert.equal(presenter.viewModel.empty, null);
    assert.equal(server.itemsOf(conversation.state.threadId!).length, 1);
  });

  test("in a project with no tasks says so and offers to shape a first feature", () => {
    const { conversation, presenter } = setup({ projectName: "harness", noTasks: true });
    conversation.attach("p1", null);
    assert.equal(presenter.viewModel.empty?.heading, "harness has no tasks yet");
    assert.equal(presenter.viewModel.empty?.starters[0], FIRST_FEATURE_STARTER);
  });
});

describe("the composer", () => {
  test("sends the draft with Enter, breaks a line with Shift+Enter, and is empty after", async () => {
    const { conversation, presenter } = setup();
    conversation.attach("p1", null);
    presenter.setDraft("Hello");
    assert.equal(presenter.viewModel.composer.canSend, true);
    assert.equal(presenter.handleComposerKey({ key: "Enter", shiftKey: true }), false);
    assert.equal(presenter.handleComposerKey({ key: "Enter", shiftKey: false }), true);
    await settled();
    assert.equal(presenter.viewModel.composer.draft, "");
    assert.equal(presenter.viewModel.rows.length, 1);
  });

  test("cannot send an empty draft", () => {
    const { conversation, presenter } = setup();
    conversation.attach("p1", null);
    presenter.setDraft("   ");
    assert.equal(presenter.viewModel.composer.canSend, false);
    assert.equal(presenter.handleComposerKey({ key: "Enter", shiftKey: false }), true);
    assert.equal(presenter.viewModel.rows.length, 0);
  });

  test("turns Send into Stop while an answer is on its way, and Enter does not send then", async () => {
    const { conversation, presenter, server } = setup();
    server.seedThread("p1", { threadId: "t1" }, [person("Q")]);
    conversation.attach("p1", "t1");
    await settled();
    assert.equal(presenter.viewModel.composer.mode, "send");
    server.setTurn("t1", "running");
    assert.equal(presenter.viewModel.composer.mode, "stop");
    presenter.setDraft("more");
    presenter.handleComposerKey({ key: "Enter", shiftKey: false });
    assert.equal(server.itemsOf("t1").length, 1);
    await presenter.stop();
    assert.deepEqual(server.stops, ["t1"]);
  });

  test("is disabled with its own placeholder while the product owner is off, and back when it is on", () => {
    const { conversation, presenter } = setup();
    conversation.attach("p1", null);
    presenter.setOff(true);
    assert.deepEqual([presenter.viewModel.composer.disabled, presenter.viewModel.composer.placeholder], [true, OFF_PLACEHOLDER]);
    presenter.setDraft("hi");
    assert.equal(presenter.viewModel.composer.canSend, false);
    presenter.setOff(false);
    assert.equal(presenter.viewModel.composer.disabled, false);
  });

  test("says Continue this thread on a thread that has begun", async () => {
    const { conversation, presenter, server } = setup();
    server.seedThread("p1", { threadId: "t1" }, [person("Q")]);
    conversation.attach("p1", "t1");
    await settled();
    assert.equal(presenter.viewModel.composer.placeholder, "Continue this thread");
  });

  test("keeps the typed text and shows the refusal when the send is refused", async () => {
    const { conversation, presenter, server } = setup();
    server.seedThread("p1", { threadId: "t1", turn: "running" });
    conversation.attach("p1", "t1");
    await settled();
    presenter.setDraft("more");
    await presenter.send();
    assert.equal(presenter.viewModel.composer.draft, "more");
    assert.equal(presenter.viewModel.error?.code, "THREAD_BUSY");
  });
});

describe("the transcript", () => {
  test("shows my message as sending until the server has it, then an answer with a caret as it streams", async () => {
    const { conversation, presenter, server } = setup();
    conversation.attach("p1", null);
    server.silenceEchoes(true);
    const release = server.holdNextSend();
    const sending = presenter.startWith("Hi");
    const [mine] = presenter.viewModel.rows;
    assert.ok(mine?.kind === "message");
    assert.deepEqual([mine.who, mine.sending], ["You", true]);
    release();
    await sending;
    server.publish("p1", { type: "thread", thread: { ...(await server.threads("p1"))[0]!, turn: "running" } });
    server.publish("p1", { type: "delta", threadId: conversation.state.threadId!, itemId: "a1", text: "Done: **two**" });
    const answer = presenter.viewModel.rows.at(-1)!;
    assert.deepEqual(answer.kind === "message" && [answer.who, answer.streaming, answer.blocks[0]!.kind], ["Product owner", true, "paragraph"]);
  });

  test("renders a stopped answer as stopped", async () => {
    const { conversation, presenter, server } = setup();
    server.seedThread("p1", { threadId: "t1" }, [person("Q"), message({ text: "Partial", stopped: true })]);
    conversation.attach("p1", "t1");
    await settled();
    const last = presenter.viewModel.rows.at(-1)!;
    assert.equal(last.kind === "message" && last.stopped, true);
  });

  test("shows an item this piece does not draw as a neutral line, and a notice as it is", async () => {
    const { conversation, presenter, server } = setup();
    server.seedThread("p1", { threadId: "t1" }, [
      { kind: "other", wireKind: "held_action", itemId: "h1", at: new Date(), text: "Archive task X" },
      { kind: "notice", itemId: "n1", at: new Date(), text: "Continued after a restart" },
    ]);
    conversation.attach("p1", "t1");
    await settled();
    assert.deepEqual(presenter.viewModel.rows.map((r) => [r.kind, r.kind === "other" ? r.label : ""]), [["other", "held action"], ["notice", ""]]);
  });

  test("a failed answer offers Try again, which sends the same message again", async () => {
    const { conversation, presenter, server } = setup();
    server.seedThread("p1", { threadId: "t1" }, [person("Q", { itemId: "m1" }), failure({ itemId: "f1", retryItemId: "m1" })]);
    conversation.attach("p1", "t1");
    await settled();
    const failed = presenter.viewModel.rows[1]!;
    assert.deepEqual(failed.kind === "failure" && [failed.code, failed.canRetry], ["SESSION_RESUME_FAILED", true]);
    await presenter.retry("m1");
    assert.deepEqual(server.itemsOf("t1").map((i) => i.kind === "message" && i.text), ["Q", false, "Q"]);
  });
});

describe("what the product owner is doing", () => {
  const answering = async (turn: "resuming" | "running" | "interrupted", items: ThreadItem[] = [person("Q")]) => {
    const h = setup();
    h.server.seedThread("p1", { threadId: "t1", turn }, items);
    h.conversation.attach("p1", "t1");
    await settled();
    return h;
  };

  test("says it is resuming while the session wakes", async () => {
    const { presenter } = await answering("resuming");
    assert.equal(presenter.viewModel.live?.label, "Resuming");
  });

  test("says what it reads, then that it writes", async () => {
    const { presenter, server } = await answering("running", [person("Q"), { kind: "activity", itemId: "a1", at: new Date(), text: "Reading the board" }]);
    assert.equal(presenter.viewModel.live?.label, "Reading the board");
    assert.equal(presenter.viewModel.rows.length, 1, "an activity is the live line, not a row");
    server.publish("p1", { type: "delta", threadId: "t1", itemId: "a2", text: "Here" });
    assert.equal(presenter.viewModel.live?.label, "Writing");
  });

  test("has no live line while idle", async () => {
    const { presenter } = setup();
    assert.equal(presenter.viewModel.live, null);
  });

  test("announces that it is answering and that it finished, once each, not every token", async () => {
    const { presenter, server } = await answering("running");
    const heard: string[] = [];
    presenter.subscribe((vm) => heard.at(-1) !== vm.announcement && heard.push(vm.announcement));
    server.publish("p1", { type: "delta", threadId: "t1", itemId: "a2", text: "a" });
    server.publish("p1", { type: "delta", threadId: "t1", itemId: "a2", text: "b" });
    server.setTurn("t1", "idle");
    assert.deepEqual(heard.filter((h) => h !== ""), ["Product owner is answering", "Answer finished"]);
  });

  test("knows the status of a task an answer names", () => {
    const { presenter } = setup();
    assert.equal(presenter.taskState("t9"), "waiting");
    assert.equal(presenter.taskState("zz"), "idle");
  });
});
