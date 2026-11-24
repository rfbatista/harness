import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { memoryClipboard } from "../../infrastructure/memory-clipboard";
import { memoryContextFeed } from "../../infrastructure/memory-context-feed";
import { memoryContextSource } from "../../infrastructure/memory-context-source";
import { memoryTabFront } from "../../infrastructure/memory-tab-front";
import { contextFromJson, contextJson, entryJson, serverJson, skillJson, toolJson, type SessionContextJson } from "../../testing/fixtures";
import { ContextDotPresenter } from "./context-dot-presenter";
import { ContextPresenter } from "./context-presenter";

const flush = () => new Promise<void>((resolve) => setTimeout(resolve, 0));
const use = (count: number) => ({ count, last_at: "2026-11-23T21:00:00Z" });

const base = (over: Partial<SessionContextJson> = {}) =>
  contextJson({
    entries: [entryJson({ tools: [toolJson({ name: "Bash" }), toolJson({ name: "Read" })], skills: [skillJson({ name: "alpha" }), skillJson({ name: "beta" })], mcp_servers: [serverJson({ name: "task" })] })],
    ...over,
  });

const setup = async (json = base(), live = true) => {
  const source = memoryContextSource();
  const feed = memoryContextFeed();
  source.serve(contextFromJson(json));
  const presenter = new ContextPresenter(source, "s1", { clipboard: memoryClipboard(), feed, projectId: "p1", live, timeZone: "UTC" });
  presenter.open();
  await flush();
  return { source, feed, presenter, json };
};

describe("following context.changed", () => {
  test("an announcement for the session shown reads the context again, once", async () => {
    const { source, feed } = await setup();
    feed.emit({ sessionId: "s1", reason: "usage" });
    await flush();
    assert.deepEqual(source.loaded, ["s1", "s1"]);
  });

  test("an announcement for another session fetches nothing", async () => {
    const { source, feed } = await setup();
    feed.emit({ sessionId: "other", reason: "usage" });
    await flush();
    assert.deepEqual(source.loaded, ["s1"]);
  });

  test("announcements during a fetch coalesce into one follow-up fetch", async () => {
    const { source, feed } = await setup();
    const release = source.hold();
    feed.emit({ sessionId: "s1", reason: "usage" });
    feed.emit({ sessionId: "s1", reason: "usage" });
    feed.emit({ sessionId: "s1", reason: "mcp_status" });
    release();
    await flush();
    await flush();
    assert.equal(source.loaded.length, 3);
  });

  test("close stops following", async () => {
    const { source, feed, presenter } = await setup();
    presenter.close();
    assert.equal(feed.followers("p1"), 0);
    feed.emit({ sessionId: "s1", reason: "usage" });
    await flush();
    assert.equal(source.loaded.length, 1);
  });

  test("a session that ended says so and is not followed", async () => {
    const { feed, presenter } = await setup(base(), false);
    assert.equal(presenter.viewModel.ended, true);
    assert.equal(feed.followers("p1"), 0);
  });
});

describe("a live change", () => {
  const changed = (json: SessionContextJson): SessionContextJson => ({
    ...json,
    usage: { tools: { Read: use(1) }, skills: { beta: use(1) } },
    changes: [
      { at: "2026-11-23T21:05:00Z", kind: "skill_invoked", subject: "beta", detail: "invoked from the prompt" },
      { at: "2026-11-23T21:04:00Z", kind: "tool_first_used", subject: "Read", detail: "on demand tool first used" },
    ],
  });

  test("rings the changed rows in place without moving any", async () => {
    const { source, feed, presenter, json } = await setup();
    const order = () => [presenter.viewModel.groups!.tools.sections[0]!.rows.map((r) => r.fullName), presenter.viewModel.groups!.skills.rows.map((r) => r.name)];
    const before = order();
    source.serve(contextFromJson(changed(json)));
    feed.emit({ sessionId: "s1", reason: "usage" });
    await flush();
    assert.deepEqual(order(), before);
    assert.deepEqual(presenter.viewModel.groups!.tools.sections[0]!.rows.map((r) => r.ringing), [false, true]);
    assert.deepEqual(presenter.viewModel.groups!.skills.rows.map((r) => r.ringing), [false, true]);
  });

  test("names the latest change on one live line", async () => {
    const { source, feed, presenter, json } = await setup();
    assert.equal(presenter.viewModel.live, null);
    source.serve(contextFromJson(changed(json)));
    feed.emit({ sessionId: "s1", reason: "usage" });
    await flush();
    assert.deepEqual(presenter.viewModel.live, { text: "Skill beta invoked", time: "21:05" });
  });

  test("a failed MCP server is a change the live line names", async () => {
    const { source, feed, presenter, json } = await setup();
    const failed = { ...json, entries: [entryJson({ mcp_servers: [serverJson({ name: "task", status: "failed", status_reason: "boom" })] })], changes: [{ at: "2026-11-23T21:05:00Z", kind: "mcp_failed", subject: "task", detail: "boom" }] };
    source.serve(contextFromJson(failed));
    feed.emit({ sessionId: "s1", reason: "mcp_status" });
    await flush();
    assert.equal(presenter.viewModel.live?.text, "MCP server task failed");
    assert.equal(presenter.viewModel.groups!.servers.rows[0]!.ringing, true);
  });

  test("the ring and the live line clear when dismissed, and are replaced by the next change", async () => {
    const { source, feed, presenter, json } = await setup();
    source.serve(contextFromJson(changed(json)));
    feed.emit({ sessionId: "s1", reason: "usage" });
    await flush();
    presenter.dismissLive();
    assert.equal(presenter.viewModel.live, null);
    assert.deepEqual(presenter.viewModel.groups!.skills.rows.map((r) => r.ringing), [false, false]);
    const next = changed(json);
    next.usage.tools.Bash = use(1);
    next.changes.unshift({ at: "2026-11-23T21:06:00Z", kind: "tool_first_used", subject: "Bash", detail: "x" });
    source.serve(contextFromJson(next));
    feed.emit({ sessionId: "s1", reason: "usage" });
    await flush();
    assert.deepEqual(presenter.viewModel.groups!.tools.sections[0]!.rows.map((r) => r.ringing), [true, false]);
  });

  test("lists Changes while running newest first, with the count on the heading", async () => {
    const { source, feed, presenter, json } = await setup();
    source.serve(contextFromJson(changed(json)));
    feed.emit({ sessionId: "s1", reason: "usage" });
    await flush();
    const g = presenter.viewModel.groups!.changes;
    assert.equal(g.title, "Changes while running");
    assert.equal(g.count, "2");
    assert.deepEqual(g.rows.map((r) => [r.time, r.text, r.note]), [
      ["21:05", "Skill beta invoked", "invoked from the prompt"],
      ["21:04", "Read used for the first time", "on demand tool first used"],
    ]);
  });

  test("a failed refresh keeps what is on screen", async () => {
    const { source, feed, presenter } = await setup();
    const before = presenter.viewModel;
    source.failWith(new (await import("../../application/ports")).ContextSourceError("NETWORK_ERROR", "offline"));
    feed.emit({ sessionId: "s1", reason: "usage" });
    await flush();
    assert.equal(presenter.viewModel.status, "ready");
    assert.deepEqual(presenter.viewModel.groups!.tools, before.groups!.tools);
  });
});

describe("the dot on the tab", () => {
  const dotSetup = (front: boolean) => {
    const feed = memoryContextFeed();
    const tab = memoryTabFront(front);
    const dot = new ContextDotPresenter(feed, tab, { sessionId: "s1", projectId: "p1", live: true });
    return { feed, tab, dot };
  };

  test("lights when the session's context changes while another tab is in front", () => {
    const { feed, dot } = dotSetup(false);
    assert.equal(dot.viewModel.dot, false);
    feed.emit({ sessionId: "s1", reason: "usage" });
    assert.equal(dot.viewModel.dot, true);
  });

  test("goes out when the Context tab comes to the front", () => {
    const { feed, tab, dot } = dotSetup(false);
    feed.emit({ sessionId: "s1", reason: "entry_added" });
    tab.set(true);
    assert.equal(dot.viewModel.dot, false);
  });

  test("does not light while the Context tab is in front, or for another session", () => {
    const { feed, tab, dot } = dotSetup(true);
    feed.emit({ sessionId: "s1", reason: "usage" });
    assert.equal(dot.viewModel.dot, false);
    tab.set(false);
    feed.emit({ sessionId: "other", reason: "usage" });
    assert.equal(dot.viewModel.dot, false);
  });

  test("an ended session never lights it", () => {
    const feed = memoryContextFeed();
    const dot = new ContextDotPresenter(feed, memoryTabFront(false), { sessionId: "s1", projectId: "p1", live: false });
    feed.emit({ sessionId: "s1", reason: "usage" });
    assert.equal(dot.viewModel.dot, false);
  });
});
