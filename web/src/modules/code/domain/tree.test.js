import { assert, file, test } from "../../../shared/testing/test.js";
import { markParts, matchPath, rank, shiftRanges } from "./fuzzy.js";
import { emptyTree, loadedOpenDirs, missingAncestors, openAction, revealPath, treeKey, typeAhead, visibleRows, withExpanded, withLevel } from "./tree.js";

file("code/domain/tree");

const e = (path, kind = "file", extra = {}) => ({ path, name: path.slice(path.lastIndexOf("/") + 1), kind, size: 1, target: "", change: null, zones: [], ...extra });

function tree() {
  let s = emptyTree();
  s = withLevel(s, "", [e("internal", "dir"), e("web", "dir"), e("go.mod")]);
  s = withLevel(s, "internal", [e("internal/feed", "dir"), e("internal/main.go")]);
  s = withExpanded(s, "internal", true);
  return s;
}

test("rows: the root, and under each open directory its loaded entries", () => {
  assert.deepEqual(visibleRows(tree()).map((r) => [r.entry.path, r.depth]), [
    ["internal", 0],
    ["internal/feed", 1],
    ["internal/main.go", 1],
    ["web", 0],
    ["go.mod", 0],
  ]);
});

test("APG keys: down/up, right opens then enters, left closes then climbs, Home/End, *", () => {
  const s = tree();
  assert.deepEqual(treeKey(s, "internal", "ArrowDown"), { focus: "internal/feed" });
  assert.deepEqual(treeKey(s, "go.mod", "ArrowDown"), { focus: "go.mod" });
  assert.deepEqual(treeKey(s, "internal", "ArrowUp"), { focus: "internal" });
  assert.deepEqual(treeKey(s, "web", "ArrowRight"), { expand: "web" });
  assert.deepEqual(treeKey(s, "internal", "ArrowRight"), { focus: "internal/feed" });
  assert.deepEqual(treeKey(s, "internal", "ArrowLeft"), { collapse: "internal" });
  assert.deepEqual(treeKey(s, "internal/main.go", "ArrowLeft"), { focus: "internal" });
  assert.deepEqual(treeKey(s, "web", "Home"), { focus: "internal" });
  assert.deepEqual(treeKey(s, "web", "End"), { focus: "go.mod" });
  assert.deepEqual(treeKey(s, "go.mod", "*"), { expandAll: ["web"] });
  assert.deepEqual(treeKey(s, "nowhere", "ArrowDown"), { focus: "internal" });
});

test("type-ahead finds the next row by name, wrapping", () => {
  const s = tree();
  assert.equal(typeAhead(s, "internal", "w"), "web");
  assert.equal(typeAhead(s, "web", "i"), "internal");
  assert.equal(typeAhead(s, "internal", "in"), "internal", "a longer prefix may stay");
  assert.equal(typeAhead(s, "internal", "zzz"), null);
});

test("revealing a path opens its ancestors; the missing ones are loaded first", () => {
  const s = tree();
  assert.deepEqual(missingAncestors(s, "internal/feed/feed.go"), ["internal/feed"]);
  assert.deepEqual(Object.keys(revealPath(s, "internal/feed/feed.go").expanded).sort(), ["internal", "internal/feed"]);
});

test("a re-read level keeps what is open, and forgets a directory that went away", () => {
  let s = tree();
  s = withLevel(s, "internal/feed", [e("internal/feed/feed.go")]);
  s = withExpanded(s, "internal/feed", true);
  assert.deepEqual(loadedOpenDirs(s), ["", "internal", "internal/feed"]);
  s = withLevel(s, "internal", [e("internal/main.go", "file", { change: "modified" })]);
  assert.equal(s.levels["internal/feed"], undefined);
  assert.equal(s.expanded["internal/feed"], undefined);
  assert.equal(s.expanded.internal, true);
});

test("opening: files open, a deleted file its diff, links and submodules a placeholder", () => {
  assert.equal(openAction(e("a.go")), "file");
  assert.equal(openAction(e("a.go", "file", { change: "deleted" })), "diff");
  assert.equal(openAction(e("l", "symlink")), "symlink");
  assert.equal(openAction(e("third_party/xterm", "submodule")), "submodule");
  assert.equal(openAction(e("d", "dir")), "toggle");
});

test("fuzzy: the query in order, the file name first, with the spans to mark", () => {
  const m = matchPath("internal/feed/feed.go", "fdgo");
  assert.ok(m);
  assert.deepEqual(m.ranges, [[14, 15], [17, 18], [19, 21]]);
  assert.equal(matchPath("internal/feed.go", "xyz"), null);
  const { matches } = rank(["web/src/feed.js", "internal/feed/feed.go", "internal/adapter/out/feedback/form.go"], "feedgo");
  assert.deepEqual(matches.map((x) => x.path), ["internal/feed/feed.go", "internal/adapter/out/feedback/form.go"], "a match in the name first");
  assert.equal(rank(["a", "b"], "").matches.length, 2);
});

test("fuzzy: marks split a name into marked and plain parts", () => {
  assert.deepEqual(markParts("feed.go", [[0, 2], [5, 7]]), [
    { text: "fe", mark: true },
    { text: "ed.", mark: false },
    { text: "go", mark: true },
  ]);
  assert.deepEqual(shiftRanges([[2, 4], [9, 12]], 8, 6), [[1, 4]]);
});

test("fuzzy ranks 50,000 paths quickly", () => {
  const paths = Array.from({ length: 50_000 }, (_, i) => `internal/pkg${i % 300}/sub${i % 17}/file${i}.go`);
  const t0 = performance.now();
  const { matches, total } = rank(paths, "pkg12sub3file");
  const ms = performance.now() - t0;
  assert.ok(total > 0 && matches.length <= 100);
  assert.ok(ms < 1000, `took ${ms.toFixed(0)} ms`);
});
