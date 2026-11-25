import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { ContextSourceError } from "../../application/ports";
import { memoryClipboard } from "../../infrastructure/memory-clipboard";
import { memoryContextFeed } from "../../infrastructure/memory-context-feed";
import { memoryContextSource } from "../../infrastructure/memory-context-source";
import { contextFromJson, contextJson, entryJson, factsJson, serverJson, skillJson, toolJson, type SessionContextJson } from "../../testing/fixtures";
import { ContextPresenter } from "./context-presenter";

const flush = () => new Promise<void>((resolve) => setTimeout(resolve, 0));

const make = (json: SessionContextJson = contextJson(), live = true) => {
  const source = memoryContextSource();
  const feed = memoryContextFeed();
  source.serve(contextFromJson(json));
  const presenter = new ContextPresenter(source, "s1", { clipboard: memoryClipboard(), feed, projectId: "p1", live, timeZone: "UTC" });
  return { source, feed, presenter, json };
};

describe("following the feed over the life of the tab", () => {
  test("a Retry that succeeds follows the feed like a first load that succeeded", async () => {
    const { source, feed, presenter } = make();
    source.failWith(new ContextSourceError("NETWORK_ERROR", "offline"));
    presenter.open();
    await flush();
    assert.equal(presenter.viewModel.status, "error");
    source.heal();
    presenter.retry();
    await flush();
    assert.equal(presenter.viewModel.status, "ready");
    feed.emit({ sessionId: "s1", reason: "usage" });
    await flush();
    assert.equal(source.loaded.length, 3, "the announcement after the retry refetches");
  });

  test("closing the tab before the first answer leaves no follower behind", async () => {
    const { source, feed, presenter } = make();
    const release = source.hold();
    presenter.open();
    presenter.close();
    release();
    await flush();
    assert.equal(feed.followers("p1"), 0);
  });

  test("a session that is not found or unrecorded is not followed", async () => {
    const gone = make();
    gone.source.failWith(new ContextSourceError("SESSION_NOT_FOUND", "no"));
    gone.presenter.open();
    const unrecorded = make(contextJson({ recorded: false, entries: [] }));
    unrecorded.presenter.open();
    await flush();
    assert.equal(gone.feed.followers("p1"), 0);
    assert.equal(unrecorded.feed.followers("p1"), 0);
  });

  test("an announcement does not reload a tab that is not ready", async () => {
    const { source, feed, presenter } = make();
    presenter.open();
    feed.emit({ sessionId: "s1", reason: "usage" }); // still loading: not followed yet
    await flush();
    assert.equal(source.loaded.length, 1);
  });
});

describe("what a refresh leaves alone and what it names", () => {
  const two = contextJson({
    entries: [entryJson({ tools: [toolJson({ name: "Bash" })], skills: [skillJson({ name: "alpha" })], mcp_servers: [serverJson({ name: "task", status: "unknown" }), serverJson({ name: "db", status: "unknown" })] })],
  });
  const use = (count: number) => ({ count, last_at: "2026-11-23T21:00:00Z" });

  test("an MCP server that gets connected rings its row, and only its row", async () => {
    const { source, feed, presenter, json } = make(two);
    presenter.open();
    await flush();
    const entry = entryJson({ mcp_servers: [serverJson({ name: "task", status: "connected", status_reason: "" }), serverJson({ name: "db", status: "unknown" })], tools: [toolJson({ name: "Bash" })], skills: [skillJson({ name: "alpha" })] });
    source.serve(contextFromJson({ ...json, entries: [entry], changes: [{ at: "2026-11-23T21:05:00Z", kind: "mcp_connected", subject: "task", detail: "connected" }] }));
    feed.emit({ sessionId: "s1", reason: "mcp_status" });
    await flush();
    assert.deepEqual(presenter.viewModel.groups!.servers.rows.map((r) => r.ringing), [true, false]);
    assert.equal(presenter.viewModel.live?.text, "MCP server task connected");
  });

  test("a refresh that changes nothing keeps the rings and the live line on show", async () => {
    const { source, feed, presenter, json } = make(two);
    presenter.open();
    await flush();
    const changed = { ...json, usage: { tools: { Bash: use(1) }, skills: {} }, changes: [{ at: "2026-11-23T21:05:00Z", kind: "tool_first_used" as const, subject: "Bash", detail: "x" }] };
    source.serve(contextFromJson(changed));
    feed.emit({ sessionId: "s1", reason: "usage" });
    await flush();
    feed.emit({ sessionId: "s1", reason: "usage" });
    await flush();
    assert.equal(presenter.viewModel.live?.text, "Bash used for the first time");
    assert.equal(presenter.viewModel.groups!.tools.sections[0]!.rows[0]!.ringing, true);
  });

  test("a new entry is named on the live line and joins the picker without ringing a row", async () => {
    const { source, feed, presenter, json } = make(two);
    presenter.open();
    await flush();
    const resumed = entryJson({ id: "e2", kind: "resume", at: "2026-11-24T09:12:00Z", facts: factsJson(), tools: [toolJson({ name: "Bash" })], skills: [skillJson({ name: "alpha" })], mcp_servers: two.entries[0]!.mcp_servers });
    source.serve(contextFromJson({ ...json, entries: [...json.entries, resumed], changes: [{ at: "2026-11-24T09:12:00Z", kind: "entry_added", subject: "e2", detail: "resumed" }] }));
    feed.emit({ sessionId: "s1", reason: "entry_added" });
    await flush();
    assert.equal(presenter.viewModel.live?.text, "New entry stored");
    assert.equal(presenter.viewModel.picker!.options.length, 2);
    assert.equal(presenter.viewModel.groups!.servers.rows.some((r) => r.ringing), false);
  });

  test("a refresh, which no announcement asked for, still works on an ended session", async () => {
    const { source, presenter, json } = make(two, false);
    presenter.open();
    await flush();
    source.serve(contextFromJson({ ...json, usage: { tools: { Bash: use(3) }, skills: {} } }));
    await presenter.refresh();
    assert.equal(presenter.viewModel.groups!.tools.sections[0]!.rows[0]!.used, "used ×3");
    assert.equal(presenter.viewModel.ended, true);
  });
});
