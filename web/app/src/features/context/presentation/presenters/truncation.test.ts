import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { ContextSourceError } from "../../application/ports";
import { memoryClipboard } from "../../infrastructure/memory-clipboard";
import { memoryContextFeed } from "../../infrastructure/memory-context-feed";
import { memoryContextSource } from "../../infrastructure/memory-context-source";
import { contextFromJson, contextJson, entryJson, pieceJson } from "../../testing/fixtures";
import { ContextPresenter } from "./context-presenter";

const flush = () => new Promise<void>((resolve) => setTimeout(resolve, 0));
const head = "a".repeat(4096);

const setup = async () => {
  const json = contextJson({ entries: [entryJson({ id: "e1", instructions: [pieceJson({ id: "big", title: "Big", text: head, bytes: 9000, approx_tokens: 2250, truncated: true }), pieceJson({ id: "exact", title: "Exact", text: head, bytes: 4096, approx_tokens: 1024, truncated: false })] })] });
  const source = memoryContextSource();
  const feed = memoryContextFeed();
  source.serve(contextFromJson(json));
  const presenter = new ContextPresenter(source, "s1", { clipboard: memoryClipboard(), feed, projectId: "p1" });
  presenter.open();
  await flush();
  const rows = () => presenter.viewModel.groups!.instructions.rows;
  return { source, feed, presenter, rows, json };
};

describe("an instruction cut at 4 KB", () => {
  test("shows the first 4096 bytes, offers Show all, and sizes the whole piece", async () => {
    const { rows } = await setup();
    assert.equal(rows()[0]!.text.length, 4096);
    assert.equal(rows()[0]!.canShowAll, true);
    assert.equal(rows()[0]!.size, "8.8 KB · ≈2250 tokens");
  });

  test("a piece of exactly 4 KB is whole: no Show all", async () => {
    const { rows } = await setup();
    assert.equal(rows()[1]!.canShowAll, false);
    assert.equal(rows()[1]!.size, "4 KB · ≈1024 tokens");
  });

  test("while the full text loads, Show all is not offered twice and the head stays", async () => {
    const { source, presenter, rows } = await setup();
    source.servePiece("s1", "e1", "big", "x".repeat(9000));
    const first = presenter.showAll("big");
    assert.equal(rows()[0]!.fullText, "loading");
    assert.equal(rows()[0]!.text.length, 4096);
    await presenter.showAll("big");
    await first;
    assert.deepEqual(source.fetched, ["s1/e1/big"]);
    assert.equal(rows()[0]!.text.length, 9000);
  });

  test("a missing piece marks it failed and the other piece is untouched", async () => {
    const { presenter, rows } = await setup();
    await presenter.showAll("big"); // nothing served: CONTEXT_PIECE_NOT_FOUND
    assert.equal(rows()[0]!.fullText, "failed");
    assert.equal(rows()[1]!.fullText, "idle");
  });

  test("the full text survives a live refresh", async () => {
    const { source, feed, presenter, rows, json } = await setup();
    source.servePiece("s1", "e1", "big", "full text");
    await presenter.showAll("big");
    source.serve(contextFromJson({ ...json, usage: { tools: {}, skills: { x: { count: 1, last_at: "2026-11-23T21:00:00Z" } } } }));
    feed.emit({ sessionId: "s1", reason: "usage" });
    await flush();
    assert.equal(rows()[0]!.text, "full text");
    assert.equal(rows()[0]!.canShowAll, false);
  });

  test("search finds a sentence only the full text holds, once it was fetched", async () => {
    const { source, presenter, rows } = await setup();
    source.servePiece("s1", "e1", "big", `${head} needle`);
    presenter.search("needle");
    assert.equal(presenter.viewModel.groups!.instructions.count, "0 of 2");
    await presenter.showAll("big");
    assert.deepEqual(rows().map((r) => r.id), ["big"]);
    assert.equal(presenter.viewModel.groups!.instructions.count, "1 of 2");
  });

  test("a piece the server fails to give in full leaves the tab ready", async () => {
    const { source, presenter } = await setup();
    source.failWith(new ContextSourceError("CONTEXT_ENTRY_NOT_FOUND", "gone"));
    await presenter.showAll("big");
    assert.equal(presenter.viewModel.status, "ready");
  });
});
