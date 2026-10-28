import { assert, file, test } from "../../../shared/testing/test.js";
import { diffRows, rowOfLineIn } from "../domain/rows.js";
import { makeWorld, manyChangedFilesWorld } from "../testing/fixtures.js";
import { diffPreparer, serve } from "./diff-prep.js";
import { memoryCode } from "./memory-gateway.js";
import { inlinePreparer, workerPreparer } from "./preparer.js";

file("code/infrastructure/diff-prep");

/** Marks every word of a line as a keyword, so a test can see which rows were highlighted. */
const markHighlighter = (languages = ["javascript", "go"]) => ({
  supports: (l) => languages.includes(l),
  tokenize: (_l, text) => text.split("\n").map((t) => (t ? [{ text: t, tok: "keyword" }] : [])),
});

const wirePage = (world, q = { workspaceId: "ws1", scope: "all" }) => memoryCode(world).wire.diff(q);
const layouts = { split: { split: true }, unified: { split: false } };

test("the worker's module answers a page in page order, each file with its rows and tokens, hunks left behind", () => {
  const prep = diffPreparer(markHighlighter());
  const { diff, files } = prep.prepare(wirePage(makeWorld()), layouts.unified);
  assert.deepEqual(diff.files.map((f) => f.path), ["README.md", "docs/old.md", "internal/code/browser.go", "internal/feed/feed.go", "notes/todo.md"]);
  assert.ok(diff.files.every((f) => f.hunks === null), "the main thread never receives hunks");
  assert.equal(diff.fileCount, 5);
  const all = [...files()];
  const feed = all.find((f) => f.path === "internal/feed/feed.go");
  assert.equal(feed.split, false);
  assert.ok(feed.rows.some((r) => r.type === "hunk"));
  assert.ok(feed.rows.filter((r) => r.type === "diff").every((r) => r.tokens), "go is highlighted");
  const readme = all.find((f) => f.path === "README.md");
  assert.equal(readme.plain, true, "markdown has no grammar here");
  assert.ok(readme.rows.every((r) => r.type === "hunk" || !r.tokens));
  assert.equal(diff.files.find((f) => f.path === "internal/feed/feed.go").hasHunks, true);
  assert.ok(diff.files.find((f) => f.path === "internal/feed/feed.go").largest > 0);
});

test("the rows follow the layout: split pairs, unless the file is over the split cap", () => {
  const prep = diffPreparer(markHighlighter());
  const { files } = prep.prepare(wirePage(makeWorld()), layouts.split);
  const rows = Object.fromEntries([...files()].map((f) => [f.path, f]));
  assert.equal(rows["internal/feed/feed.go"].split, true);
  assert.ok(rows["internal/feed/feed.go"].rows.some((r) => r.type === "split"));
  assert.deepEqual([...prep.relayout(layouts.unified)].map((f) => f.split).includes(true), false);
  assert.deepEqual(rowsOfKinds(rows["internal/feed/feed.go"].rows), rowsOfKinds(diffRows(wirePage(makeWorld()).files.find((f) => f.path === "internal/feed/feed.go").hunks.map(toHunk), "split")));
});

const toHunk = (h) => ({ oldStart: h.old_start, oldLines: h.old_lines, newStart: h.new_start, newLines: h.new_lines, header: h.header, lines: h.lines.map((l) => ({ kind: l.kind, old: l.old, new: l.new, text: l.text, noNewline: !!l.no_newline })) });
const rowsOfKinds = (rows) => rows.map((r) => r.type);

test("a file over the row cap is drawn plain, one without a grammar too", () => {
  const world = makeWorld({ limits: { perFile: 100_000, total: 100_000 } });
  world.workspaces.ws1.disk["web/big.js"] = Array.from({ length: 10_500 }, (_, i) => `const a${i} = ${i};`).join("\n") + "\n";
  world.workspaces.ws1.untracked.push("web/big.js");
  const prep = diffPreparer(markHighlighter());
  const { files } = prep.prepare(wirePage(world, { workspaceId: "ws1", scope: "working" }), layouts.unified);
  const big = [...files()].find((f) => f.path === "web/big.js");
  assert.deepEqual([big.plain, big.rows.length > 10_000, big.rows.some((r) => r.tokens)], [true, true, false]);
});

test("a file counts as changed when its hunks differ from the page seen before, and a new file always does", () => {
  const world = makeWorld();
  const memory = memoryCode(world);
  const prep = diffPreparer(markHighlighter());
  const first = [...prep.prepare(memory.wire.diff({ workspaceId: "ws1" }), layouts.unified).files()];
  assert.ok(first.every((f) => f.changed));
  memory.change("ws1", { "web/new.js": "x\n" }, { untracked: ["web/new.js"] });
  memory.change("ws1", { "README.md": "# harness\n\nchanged again\n" });
  const second = Object.fromEntries([...prep.prepare(memory.wire.diff({ workspaceId: "ws1" }), layouts.unified).files()].map((f) => [f.path, f.changed]));
  assert.deepEqual(second, { "README.md": true, "docs/old.md": false, "internal/code/browser.go": false, "internal/feed/feed.go": false, "notes/todo.md": false, "web/new.js": true });
});

test("a malformed page is a BAD_RESPONSE, and the message handler says so with the id it answers", async () => {
  const sent = [];
  const handle = serve(diffPreparer(markHighlighter()), (m) => sent.push(m));
  handle({ type: "prepare", id: 7, page: { files: "nope" }, layout: layouts.unified });
  assert.deepEqual(sent.map((m) => [m.type, m.id, m.code]), [["error", 7, "BAD_RESPONSE"]]);
});

test("messages: a diff, then one file at a time in page order, then done", async () => {
  const sent = [];
  const handle = serve(diffPreparer(markHighlighter()), (m) => sent.push(m), () => new Promise((r) => setTimeout(r, 0)));
  handle({ type: "prepare", id: 1, page: wirePage(manyChangedFilesWorld(3), { workspaceId: "ws1", scope: "working" }), layout: layouts.unified });
  assert.deepEqual(sent.map((m) => m.type), ["diff", "file"], "the first file is out before the second is made");
  await new Promise((r) => setTimeout(r, 30));
  assert.deepEqual(sent.map((m) => m.type), ["diff", "file", "file", "file", "file", "file", "done"]);
  assert.ok(sent.every((m) => m.id === 1));
  assert.deepEqual(sent.filter((m) => m.type === "file").map((m) => m.path), ["README.md", "bulk/d0000/f0000.js", "bulk/d0000/f0001.js", "bulk/d0000/f0002.js", "notes/todo.md"]);
});

test("the in-process preparer resolves the page after every file, streaming each to onFile", async () => {
  const preparer = inlinePreparer(markHighlighter());
  const seen = [];
  const diff = await preparer.prepare(wirePage(makeWorld()), layouts.unified, { onFile: (f) => seen.push(f.path) });
  assert.equal(seen.length, 5);
  assert.deepEqual(seen, diff.files.map((f) => f.path));
  const again = [];
  await preparer.relayout(layouts.split, { onFile: (f) => again.push([f.path, f.split, f.changed]) });
  assert.ok(again.some(([, split]) => split));
  assert.ok(again.every(([, , changed]) => changed === false));
  await assert.rejects(preparer.prepare({ files: 1 }, layouts.unified), "BAD_RESPONSE");
});

test("the same messages through a real worker give the same answers", async () => {
  // The built bundle: run `go run ./cmd/webbuild` first.
  const url = new URL("../../../../../internal/adapter/in/web/static/code-worker.js", import.meta.url);
  const preparer = workerPreparer(url);
  try {
    const page = wirePage(makeWorld());
    const seen = [];
    const diff = await preparer.prepare(page, layouts.split, { onFile: (f) => seen.push(f) });
    const local = diffPreparer(markHighlighter(["go"])).prepare(page, layouts.split);
    assert.deepEqual(diff.files.map((f) => f.path), local.diff.files.map((f) => f.path));
    assert.deepEqual(seen.map((f) => f.path), diff.files.map((f) => f.path));
    const feed = seen.find((f) => f.path === "internal/feed/feed.go");
    assert.equal(feed.split, true);
    assert.ok(feed.rows.some((r) => r.type === "split" && (r.right?.tokens ?? r.left?.tokens)), "highlighted by the worker's own highlight.js");
    assert.ok(Number.isInteger(rowOfLineIn(feed.rows, "R", 7)));
    await assert.rejects(preparer.prepare({ nope: true }, layouts.unified), "BAD_RESPONSE");
  } finally {
    preparer.destroy();
  }
});
