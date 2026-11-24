import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { memoryClipboard } from "../../infrastructure/memory-clipboard";
import { memoryContextFeed } from "../../infrastructure/memory-context-feed";
import { memoryContextSource } from "../../infrastructure/memory-context-source";
import { contextFromJson, contextJson, entryJson, factsJson, pieceJson, type SessionContextJson } from "../../testing/fixtures";
import { ContextPresenter } from "./context-presenter";

const flush = () => new Promise<void>((resolve) => setTimeout(resolve, 0));

const started = entryJson({ id: "e1", kind: "start", at: "2026-11-23T20:49:00Z", facts: factsJson({ branch: "first" }), instructions: [pieceJson({ id: "p1", version: "v12", text: "old text", truncated: true })] });
const resumed = entryJson({
  id: "e2",
  kind: "resume",
  at: "2026-11-24T09:12:00Z",
  facts: factsJson({ branch: "second" }),
  instructions: [pieceJson({ id: "p1", version: "v13", text: "new text", changed: { from_version: "v12", to_version: "v13" } }), pieceJson({ id: "p2", title: "Branch gate" })],
});

const setup = async (json: SessionContextJson = contextJson({ entries: [started, resumed] })) => {
  const source = memoryContextSource();
  const feed = memoryContextFeed();
  source.serve(contextFromJson(json));
  const presenter = new ContextPresenter(source, "s1", { clipboard: memoryClipboard(), feed, projectId: "p1", timeZone: "UTC" });
  presenter.open();
  await flush();
  return { source, feed, presenter, json };
};

const branch = (p: ContextPresenter) => p.viewModel.facts.find((f) => f.label === "Worktree & branch")!.value;

describe("a resumed session", () => {
  test("shows the latest entry by default, with a picker of every entry", async () => {
    const { presenter } = await setup();
    assert.match(branch(presenter), /second$/);
    assert.deepEqual(presenter.viewModel.picker, {
      options: [
        { id: "e1", label: "Started 20:49" },
        { id: "e2", label: "Resumed 09:12" },
      ],
      selected: "e2",
    });
  });

  test("a session with one entry shows no picker", async () => {
    const { presenter } = await setup(contextJson({ entries: [started] }));
    assert.equal(presenter.viewModel.picker, null);
  });

  test("picking an earlier entry shows its facts and groups", async () => {
    const { presenter } = await setup();
    presenter.selectEntry("e1");
    assert.match(branch(presenter), /first$/);
    assert.equal(presenter.viewModel.picker!.selected, "e1");
    assert.deepEqual(presenter.viewModel.groups!.instructions.rows.map((r) => r.id), ["p1"]);
  });

  test("marks a piece that changed in this entry, and only that one", async () => {
    const { presenter } = await setup();
    const rows = presenter.viewModel.groups!.instructions.rows;
    assert.equal(rows[0]!.changed, "changed in this entry · v12 → v13");
    assert.equal(rows[1]!.changed, "");
    presenter.selectEntry("e1");
    assert.equal(presenter.viewModel.groups!.instructions.rows[0]!.changed, "");
  });

  test("Show all fetches the picked entry's piece", async () => {
    const { source, presenter } = await setup();
    presenter.selectEntry("e1");
    source.servePiece("s1", "e1", "p1", "old text and more");
    await presenter.showAll("p1");
    assert.deepEqual(source.fetched, ["s1/e1/p1"]);
    assert.equal(presenter.viewModel.groups!.instructions.rows[0]!.text, "old text and more");
    presenter.selectEntry("e2");
    assert.equal(presenter.viewModel.groups!.instructions.rows[0]!.text, "new text");
  });

  test("a picked entry that does not exist changes nothing", async () => {
    const { presenter } = await setup();
    presenter.selectEntry("nope");
    assert.equal(presenter.viewModel.picker!.selected, "e2");
  });
});

describe("a resume that arrives while the tab is open", () => {
  const newer = entryJson({ id: "e3", kind: "resume", at: "2026-11-24T10:00:00Z", facts: factsJson({ branch: "third" }) });

  test("becomes the default for a person who is looking at the latest", async () => {
    const { source, feed, presenter, json } = await setup();
    source.serve(contextFromJson({ ...json, entries: [started, resumed, newer] }));
    feed.emit({ sessionId: "s1", reason: "entry_added" });
    await flush();
    assert.match(branch(presenter), /third$/);
    assert.equal(presenter.viewModel.picker!.selected, "e3");
  });

  test("does not take over a person who picked an earlier entry", async () => {
    const { source, feed, presenter, json } = await setup();
    presenter.selectEntry("e1");
    source.serve(contextFromJson({ ...json, entries: [started, resumed, newer] }));
    feed.emit({ sessionId: "s1", reason: "entry_added" });
    await flush();
    assert.match(branch(presenter), /first$/);
    assert.equal(presenter.viewModel.picker!.options.length, 3);
  });

  test("picking the latest again goes back to following it", async () => {
    const { source, feed, presenter, json } = await setup();
    presenter.selectEntry("e1");
    presenter.selectEntry("e2");
    source.serve(contextFromJson({ ...json, entries: [started, resumed, newer] }));
    feed.emit({ sessionId: "s1", reason: "entry_added" });
    await flush();
    assert.equal(presenter.viewModel.picker!.selected, "e3");
  });
});
