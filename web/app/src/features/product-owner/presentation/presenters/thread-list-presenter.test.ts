import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { Conversation } from "../../application/conversation";
import { ThreadList } from "../../application/thread-list";
import { memoryProductOwner } from "../../infrastructure/memory-product-owner";
import { person, summary } from "../../testing/fixtures";
import { AboutPresenter, startedText } from "./about-presenter";
import { ThreadListPresenter, dayLabel, rowTime } from "./thread-list-presenter";

const settled = () => new Promise((resolve) => setImmediate(resolve));
const NOW = new Date(2026, 10, 23, 15, 0);
const at = (day: number, hour = 10) => new Date(2026, 10, day, hour, 0);

const setup = async () => {
  const server = memoryProductOwner();
  server.seedProject("p1", true);
  server.seedThread("p1", summary({ threadId: "a", title: "What was done this week", lastActivityAt: at(23, 9), lastMessageExcerpt: "Child 1 is in review", startedAt: at(23, 8) }), [person("Q")], "sess-a");
  server.seedThread("p1", summary({ threadId: "b", title: "Roadmap", lastActivityAt: at(23, 14), lastMessageExcerpt: "Two options", turn: "running" }));
  server.seedThread("p1", summary({ threadId: "c", title: "Yesterday's idea", lastActivityAt: at(22, 18) }));
  server.seedThread("p1", summary({ threadId: "d", title: "Old", lastActivityAt: at(10, 18), lastMessageExcerpt: "Archive the API" }));
  const conversation = new Conversation({ gateway: server, events: server });
  const list = new ThreadList({ gateway: server, events: server });
  conversation.attach("p1", "a");
  list.attach("p1");
  await settled();
  const presenter = new ThreadListPresenter(list, conversation, { now: () => NOW.getTime() });
  return { server, conversation, list, presenter, about: new AboutPresenter(conversation, list) };
};

describe("the thread list", () => {
  test("groups threads by day, newest first, with each row's title, time and excerpt", async () => {
    const { presenter } = await setup();
    assert.deepEqual(presenter.viewModel.groups.map((g) => [g.label, g.rows.map((r) => r.threadId)]), [["Today", ["b", "a"]], ["Yesterday", ["c"]], ["Tue 10 Nov", ["d"]]]);
    const roadmap = presenter.viewModel.groups[0]!.rows[0]!;
    assert.deepEqual({ ...roadmap }, { threadId: "b", title: "Roadmap", time: "14:00", excerpt: "Two options", live: true, current: false, href: "/projects/p1/product-owner/threads/b" });
    assert.equal(presenter.viewModel.groups[2]!.rows[0]!.time, "10 Nov");
  });

  test("marks the thread on screen, and a live dot only on a thread that answers", async () => {
    const { presenter } = await setup();
    const rows = presenter.viewModel.groups.flatMap((g) => g.rows);
    assert.deepEqual(rows.filter((r) => r.current).map((r) => r.threadId), ["a"]);
    assert.deepEqual(rows.filter((r) => r.live).map((r) => r.threadId), ["b"]);
  });

  test("filters by title and by excerpt, case-insensitively, and says when nothing matches", async () => {
    const { presenter } = await setup();
    presenter.setQuery("ROADMAP");
    assert.deepEqual(presenter.viewModel.groups.flatMap((g) => g.rows.map((r) => r.threadId)), ["b"]);
    presenter.setQuery("archive the");
    assert.deepEqual(presenter.viewModel.groups.flatMap((g) => g.rows.map((r) => r.threadId)), ["d"]);
    presenter.setQuery("zzz");
    assert.equal(presenter.viewModel.empty, "No thread matches.");
    presenter.setQuery("");
    assert.equal(presenter.viewModel.empty, null);
  });

  test("a thread event moves a row and its live dot as it happens", async () => {
    const { presenter, server } = await setup();
    server.setTurn("b", "idle");
    assert.equal(presenter.viewModel.groups[0]!.rows[0]!.live, false);
    await server.renameThread("c", "Renamed");
    assert.equal(presenter.viewModel.groups.flatMap((g) => g.rows).find((r) => r.threadId === "c")?.title, "Renamed");
  });

  test("picking a thread opens it; New thread starts a fresh one", async () => {
    const { presenter, conversation } = await setup();
    presenter.open("b");
    await settled();
    assert.equal(conversation.state.threadId, "b");
    presenter.newThread();
    assert.equal(conversation.state.threadId, null);
    assert.equal(presenter.viewModel.groups.flatMap((g) => g.rows).some((r) => r.current), false);
  });

  test("says there are no threads yet", async () => {
    const server = memoryProductOwner();
    server.seedProject("p1", true);
    const conversation = new Conversation({ gateway: server, events: server });
    const list = new ThreadList({ gateway: server, events: server });
    conversation.attach("p1", null);
    list.attach("p1");
    await settled();
    assert.match(new ThreadListPresenter(list, conversation, { now: () => NOW.getTime() }).viewModel.empty ?? "", /No threads yet/);
  });

  test("dates read as the person says them", () => {
    assert.equal(dayLabel(at(23), NOW), "Today");
    assert.equal(dayLabel(at(22), NOW), "Yesterday");
    assert.equal(dayLabel(new Date(2025, 10, 3), NOW), "Mon 3 Nov 2025");
    assert.equal(rowTime(at(23, 9), NOW), "09:00");
  });
});

describe("about this thread", () => {
  test("tells its title, when it started, how many messages, and its link; the Context tab has no page to link to yet", async () => {
    const { about } = await setup();
    assert.deepEqual({ ...about.viewModel }, {
      available: true,
      title: "What was done this week",
      started: startedText(at(23, 8)),
      messages: "1 message",
      link: "/projects/p1/product-owner/threads/a",
      contextHref: null,
      error: "",
    });
  });

  test("renames the thread, and shows the server's refusal of an empty title", async () => {
    const { about, conversation } = await setup();
    assert.equal(await about.rename("Weekly review"), true);
    assert.equal(conversation.state.title, "Weekly review");
    assert.equal(await about.rename(" "), false);
    assert.match(about.viewModel.error, /empty or too long/);
  });

  test("has nothing to tell on a fresh thread", async () => {
    const { about, conversation } = await setup();
    conversation.newThread();
    assert.equal(about.viewModel.available, false);
    assert.equal(about.viewModel.link, "");
    assert.equal(about.viewModel.contextHref, null);
  });
});
