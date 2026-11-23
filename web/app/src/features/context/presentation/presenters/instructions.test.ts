import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { ContextSourceError } from "../../application/ports";
import { memoryContextSource } from "../../infrastructure/memory-context-source";
import { contextFromJson, contextJson, entryJson, pieceJson } from "../../testing/fixtures";
import { ContextPresenter } from "./context-presenter";

const flush = () => new Promise<void>((resolve) => setTimeout(resolve, 0));

const setup = async (pieces = [pieceJson()]) => {
  const json = contextJson({ entries: [entryJson({ id: "e1", instructions: pieces })] });
  const source = memoryContextSource();
  source.serve(contextFromJson(json));
  const presenter = new ContextPresenter(source, "s1");
  presenter.open();
  await flush();
  return { source, presenter };
};

const rows = (p: ContextPresenter) => p.viewModel.groups!.instructions.rows;

describe("Instructions", () => {
  test("lists the pieces in the order sent, with the count on the heading", async () => {
    const { presenter } = await setup([pieceJson({ id: "a", title: "Repository prompt" }), pieceJson({ id: "b", title: "Task brief" }), pieceJson({ id: "c", title: "Branch gate" })]);
    const group = presenter.viewModel.groups!.instructions;
    assert.deepEqual(group.rows.map((r) => r.title), ["Repository prompt", "Task brief", "Branch gate"]);
    assert.equal(group.title, "Instructions");
    assert.equal(group.count, "3");
  });

  test("each row carries source, version, bytes and approximate tokens", async () => {
    const { presenter } = await setup([pieceJson({ source: "Instruction files · harness", path: "agents/CLAUDE.md", version: "v12", bytes: 1300, approx_tokens: 325 })]);
    const [row] = rows(presenter);
    assert.equal(row!.source, "Instruction files · harness");
    assert.equal(row!.path, "agents/CLAUDE.md");
    assert.equal(row!.version, "v12");
    assert.equal(row!.size, "1.3 KB · ≈325 tokens");
  });

  test("sizes read in bytes, KB and MB", async () => {
    const { presenter } = await setup([pieceJson({ id: "a", bytes: 812 }), pieceJson({ id: "b", bytes: 2048 }), pieceJson({ id: "c", bytes: 3 * 1024 * 1024 })]);
    assert.deepEqual(rows(presenter).map((r) => r.size.split(" · ")[0]), ["812 B", "2 KB", "3 MB"]);
  });

  test("a piece without a path or a version shows neither", async () => {
    const { presenter } = await setup([pieceJson({ path: "", version: "" })]);
    assert.equal(rows(presenter)[0]!.path, "");
    assert.equal(rows(presenter)[0]!.version, "");
  });

  test("a row opens to show its text in place, and closes", async () => {
    const { presenter } = await setup([pieceJson({ id: "p1", text: "Build the tab." })]);
    assert.equal(rows(presenter)[0]!.open, false);
    presenter.togglePiece("p1");
    assert.equal(rows(presenter)[0]!.open, true);
    assert.equal(rows(presenter)[0]!.text, "Build the tab.");
    presenter.togglePiece("p1");
    assert.equal(rows(presenter)[0]!.open, false);
  });

  test("a piece that is not cut offers no Show all", async () => {
    const { presenter } = await setup([pieceJson({ truncated: false })]);
    assert.equal(rows(presenter)[0]!.canShowAll, false);
  });

  test("Show all fetches the full text of that entry's piece once and shows it", async () => {
    const { source, presenter } = await setup([pieceJson({ id: "p1", truncated: true, text: "head", bytes: 9000 })]);
    source.servePiece("s1", "e1", "p1", "head and the rest");
    assert.equal(rows(presenter)[0]!.canShowAll, true);
    presenter.togglePiece("p1");
    await presenter.showAll("p1");
    assert.equal(rows(presenter)[0]!.text, "head and the rest");
    assert.equal(rows(presenter)[0]!.canShowAll, false);
    await presenter.showAll("p1");
    assert.deepEqual(source.fetched, ["s1/e1/p1"]);
  });

  test("a failed fetch marks that piece only, keeps its head and can be retried", async () => {
    const { source, presenter } = await setup([pieceJson({ id: "p1", truncated: true, text: "head" }), pieceJson({ id: "p2" })]);
    source.failWith(new ContextSourceError("NETWORK_ERROR", "offline"));
    await presenter.showAll("p1");
    assert.equal(rows(presenter)[0]!.fullText, "failed");
    assert.equal(rows(presenter)[0]!.text, "head");
    assert.equal(rows(presenter)[0]!.canShowAll, true);
    assert.equal(rows(presenter)[1]!.fullText, "idle");
    assert.equal(presenter.viewModel.status, "ready");
    source.heal();
    source.servePiece("s1", "e1", "p1", "all");
    await presenter.showAll("p1");
    assert.equal(rows(presenter)[0]!.text, "all");
  });

  test("the group can be collapsed and opened at once", async () => {
    const { presenter } = await setup();
    assert.equal(presenter.viewModel.groups!.instructions.open, true);
    presenter.toggleGroup("instructions");
    assert.equal(presenter.viewModel.groups!.instructions.open, false);
    presenter.openGroup("instructions");
    presenter.openGroup("instructions");
    assert.equal(presenter.viewModel.groups!.instructions.open, true);
  });

  test("a group asked to open before the context has loaded is open once it has", async () => {
    const source = memoryContextSource();
    source.serve(contextFromJson(contextJson()));
    const presenter = new ContextPresenter(source, "s1");
    presenter.toggleGroup("instructions");
    presenter.openGroup("instructions");
    presenter.open();
    await flush();
    assert.equal(presenter.viewModel.groups!.instructions.open, true);
  });
});
