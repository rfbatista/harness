import { assert, file, test } from "../../../shared/testing/test.js";
import { ancestors, changeMark, dirMark, formatSize, languageName } from "./code.js";
import { gitCommand, lineAnchor, parseDiffHash, rowOfLine, rowOfNewLine, sidesOf, slugOf, slugs, splitRows, startsCollapsed, statOf, withheldCopy } from "./diff.js";
import { bannerLine, problemOf } from "./errors.js";
import { clampRange, digits, parseRange, parseRangeHash, rangeHash, splitLines, windowOf, scrollTopFor } from "./lines.js";
import { onChange, onEnded, onReload, stateWord } from "./live.js";
import { hrefOf, parseHref, permalink, repositoryHref, route } from "./routes.js";
import { resolveTarget } from "./symlink.js";
import { literalDir, mapView, zoneDot, zoneIndex, zoneTags } from "./zones.js";

file("code/domain");

const line = (kind, old, nw, text = "x") => ({ kind, old, new: nw, text, noNewline: false });

test("change marks: a letter and a word per state, a dot for a directory with changes", () => {
  assert.deepEqual(changeMark("renamed"), { change: "renamed", letter: "R", word: "Renamed" });
  assert.deepEqual(changeMark("untracked"), { change: "untracked", letter: "U", word: "Untracked" });
  assert.deepEqual(dirMark("modified"), { change: "contains", letter: "", word: "Contains changes" });
  assert.equal(dirMark(null), null);
  assert.equal(changeMark("exploded"), null);
});

test("paths, sizes and language names", () => {
  assert.deepEqual(ancestors("a/b/c.go"), ["a", "a/b"]);
  assert.deepEqual(ancestors("c.go"), []);
  assert.equal(formatSize(512), "512 B");
  assert.equal(formatSize(6246), "6.1 KB");
  assert.equal(formatSize(2.4 * 1024 * 1024), "2.4 MB");
  assert.equal(languageName("go"), "Go");
  assert.equal(languageName("elixir"), "Elixir");
});

test("lines: a trailing newline ends the last line; CRLF is read as LF", () => {
  assert.deepEqual(splitLines("a\nb\n"), ["a", "b"]);
  assert.deepEqual(splitLines("a\r\nb"), ["a", "b"]);
  assert.deepEqual(splitLines(""), []);
  assert.deepEqual(splitLines("\n"), [""]);
  assert.equal(digits(9), 1);
  assert.equal(digits(10_000), 5);
});

test("Go to line reads a line or a range, and clamps to the file saying so", () => {
  assert.deepEqual(parseRange("120"), { start: 120, end: 120 });
  assert.deepEqual(parseRange(" 140 - 120 "), { start: 120, end: 140 });
  assert.equal(parseRange("abc"), null);
  assert.equal(parseRange("0"), null);
  const { range, note } = clampRange({ start: 90, end: 140 }, 100);
  assert.deepEqual(range, { start: 90, end: 100 });
  assert.equal(note, "The file has 100 lines; went to line 100.");
  assert.equal(clampRange({ start: 5, end: 5 }, 100).note, "");
});

test("line hashes round-trip", () => {
  assert.equal(rangeHash({ start: 45, end: 49 }), "#L45-L49");
  assert.equal(rangeHash({ start: 12, end: 12 }), "#L12");
  assert.deepEqual(parseRangeHash("#L45-L49"), { start: 45, end: 49 });
  assert.deepEqual(parseRangeHash("#L12"), { start: 12, end: 12 });
  assert.equal(parseRangeHash("#diff-a-R1"), null);
});

test("the virtual window covers the viewport and the overscan, clamped to the list", () => {
  assert.deepEqual(windowOf({ count: 10_000, rowHeight: 20, scrollTop: 0, viewport: 400, overscan: 10 }), { first: 0, last: 30 });
  assert.deepEqual(windowOf({ count: 10_000, rowHeight: 20, scrollTop: 20_000, viewport: 400, overscan: 10 }), { first: 990, last: 1030 });
  assert.deepEqual(windowOf({ count: 50, rowHeight: 20, scrollTop: 5000, viewport: 400, overscan: 10 }), { first: 50, last: 50 });
  assert.deepEqual(windowOf({ count: 100, rowHeight: 20, scrollTop: 300, viewport: 400, offset: 200, overscan: 0 }), { first: 5, last: 25 });
  assert.equal(scrollTopFor({ index: 100, rowHeight: 20, viewport: 600 }), 1800);
});

test("diff anchors: the slug, its repeats, and hashes", () => {
  assert.equal(slugOf("internal/ports/workspaces.go"), "internal-ports-workspaces-go");
  assert.equal(slugOf("internal/application/code/browser_test.go"), "internal-application-code-browser_test-go");
  const s = slugs(["a/b.go", "a-b.go", "c.go"]);
  assert.deepEqual([...s.values()], ["a-b-go", "a-b-go-2", "c-go"]);
  assert.equal(lineAnchor("a-b-go", "R", 12), "diff-a-b-go-R12");
  assert.deepEqual(parseDiffHash("#diff-a-b-go-R12"), { slug: "a-b-go", side: "R", line: 12 });
  assert.deepEqual(parseDiffHash("#diff-a-b-go-L3"), { slug: "a-b-go", side: "L", line: 3 });
  assert.deepEqual(parseDiffHash("#diff-a-b-go"), { slug: "a-b-go", side: "", line: 0 });
  assert.equal(parseDiffHash("#L12"), null);
});

test("withheld files say why, and what can be done", () => {
  const big = { path: "a.go", status: "modified", binary: false, added: 4000, deleted: 812, hunks: null, withheld: "too_large" };
  assert.deepEqual(withheldCopy(big), { reason: "too_large", title: "Large diff, not loaded", detail: "4,812 changed lines. Loading it may slow this page.", action: "load" });
  assert.equal(withheldCopy(big, { asked: true }).title, "Too large to show here");
  assert.equal(withheldCopy(big, { asked: true }).detail, "The patch is over 4 MB.");
  assert.equal(withheldCopy({ ...big, added: 30_000 }).detail, "Over 20,000 changed lines.");
  const bin = { path: "logo.png", status: "added", binary: true, added: -1, deleted: -1, hunks: null, withheld: "binary" };
  assert.deepEqual(withheldCopy(bin, { size: 49_152 }), { reason: "binary", title: "Binary file", detail: "48 KB. Its changes are not shown as text.", action: "" });
  assert.equal(statOf(bin), null, "binary counts are never printed");
  assert.deepEqual(statOf(big), { added: "+4,000", deleted: "−812" });
  assert.equal(withheldCopy({ ...big, withheld: null, hunks: [] }), null);
});

test("the git command to read a withheld file locally fits the diff's scope", () => {
  const base = "1111111aaaaaaa";
  const head = "2222222bbbbbbb";
  assert.equal(gitCommand({ scope: "branch", base, head, path: "a/b.go" }), "git diff 1111111...2222222 -- a/b.go");
  assert.equal(gitCommand({ scope: "commit", base, head, path: "a/b.go" }), "git show 2222222 -- a/b.go");
  assert.equal(gitCommand({ scope: "working", base, head, path: "my file.go" }), "git diff HEAD -- 'my file.go'");
});

test("split pairs each run of deletions with the additions after it", () => {
  const rows = splitRows([line("context", 1, 1), line("del", 2, null), line("del", 3, null), line("add", null, 2), line("context", 4, 3), line("add", null, 4)]);
  assert.deepEqual(
    rows.map((r) => [r.left?.kind ?? "empty", r.right?.kind ?? "empty"]),
    [["context", "context"], ["del", "add"], ["del", "empty"], ["context", "context"], ["empty", "add"]],
  );
});

test("a line's row in the unified layout counts hunk headers", () => {
  const hunks = [
    { oldStart: 1, oldLines: 2, newStart: 1, newLines: 2, header: "", lines: [line("context", 1, 1), line("del", 2, null), line("add", null, 2)] },
    { oldStart: 40, oldLines: 1, newStart: 40, newLines: 1, header: "", lines: [line("context", 40, 40)] },
  ];
  assert.equal(rowOfNewLine(hunks, 2), 3);
  assert.equal(rowOfNewLine(hunks, 40), 5);
  assert.equal(rowOfNewLine(hunks, 10), 5, "a line between hunks goes to the next one shown");
  assert.equal(rowOfNewLine(hunks, 99), -1);
  assert.equal(rowOfLine(hunks, "L", 2), 2);
  const { newText, oldText, at } = sidesOf(hunks[0]);
  assert.deepEqual([newText, oldText], ["x\nx", "x\nx"]);
  assert.deepEqual(at, [{ old: 0, new: 0 }, { old: 1, new: -1 }, { old: -1, new: 1 }]);
});

test("a deleted file over 50 lines starts collapsed", () => {
  assert.equal(startsCollapsed({ status: "deleted", deleted: 51 }), true);
  assert.equal(startsCollapsed({ status: "deleted", deleted: 50 }), false);
  assert.equal(startsCollapsed({ status: "modified", deleted: 500 }), false);
});

test("errors read as the UX's banners and empty states", () => {
  assert.equal(bannerLine(problemOf("PATH_NOT_FOUND", { path: "internal/x.go", ref: "v0.9.0" })), "PATH_NOT_FOUND · internal/x.go is not on v0.9.0");
  assert.equal(problemOf("PATH_NOT_FOUND", { path: "a.go" }).action, "Show the tree root");
  assert.equal(bannerLine(problemOf("REF_NOT_FOUND", { ref: "v2" })), "REF_NOT_FOUND · v2 is not a branch or tag here");
  const none = problemOf("REF_NOT_FOUND", { repository: "harness", defaultRef: true });
  assert.deepEqual([none.as, none.title, none.text], ["empty", "No commits yet", "harness has no commits to read. Files appear here after the first commit."]);
  assert.equal(problemOf("UNAVAILABLE").title, "Code browsing is off on this server");
  assert.equal(bannerLine(problemOf("INVALID_INPUT")), "INVALID_INPUT · Use a branch, tag or commit hash");
});

test("live: a change never moves the code under a reader who scrolled", () => {
  assert.deepEqual(onChange("live", { untouched: true }), { state: "live", apply: true, announce: false });
  assert.deepEqual(onChange("live", { untouched: false }), { state: "stale", apply: false, announce: true });
  assert.deepEqual(onChange("stale", { untouched: true }), { state: "stale", apply: false, announce: false }, "announced once per episode");
  assert.deepEqual(onChange("gone", { untouched: true }), { state: "gone", apply: false, announce: false });
  assert.deepEqual(onChange("", { untouched: true }), { state: "", apply: false, announce: false });
  assert.equal(onReload("stale"), "live");
  assert.equal(onEnded("live"), "");
  assert.equal(onEnded("gone"), "gone");
  assert.equal(stateWord("stale"), "Changed on disk");
});

test("routes: session and repository URLs round-trip", () => {
  const s = route({ kind: "session", projectId: "p 1", taskId: "t1", sessionId: "s1", view: "files", path: "internal/feed/feed.go" });
  assert.equal(hrefOf(s, "#L45-L49"), "/projects/p%201/tasks/t1/sessions/s1/code/files/internal/feed/feed.go#L45-L49");
  assert.deepEqual(parseHref("/projects/p%201/tasks/t1/sessions/s1/code/files/internal/feed/feed.go", ""), s);
  const c = route({ kind: "session", projectId: "p", taskId: "t", sessionId: "s", view: "changes", scope: "branch" });
  assert.equal(hrefOf(c), "/projects/p/tasks/t/sessions/s/code/changes?scope=branch");
  assert.equal(hrefOf({ ...c, scope: "all" }), "/projects/p/tasks/t/sessions/s/code/changes");
  assert.deepEqual(parseHref("/projects/p/tasks/t/sessions/s/code/changes", "?scope=branch"), c);
  const r = route({ kind: "repository", projectId: "p", repositoryId: "r", view: "map", ref: "v0.9.0" });
  assert.equal(hrefOf(r), "/projects/p/repositories/r/code/map?ref=v0.9.0");
  assert.deepEqual(parseHref("/projects/p/repositories/r/code/map", "?ref=v0.9.0"), r);
  assert.equal(parseHref("/projects/p/repositories/r/code", "").view, "");
  assert.equal(parseHref("/projects/p/repositories/r/history", ""), null);
  assert.equal(parseHref("/projects/p/repositories/r/code/nope", ""), null);
});

test("permalinks pin the commit: ref on a repository, a hint on a session", () => {
  const r = route({ kind: "repository", projectId: "p", repositoryId: "r", view: "files", path: "a.go", ref: "main" });
  assert.equal(permalink(r, "3f9a2c1abc", "#L12"), "/projects/p/repositories/r/code/files/a.go?ref=3f9a2c1abc#L12");
  const s = route({ kind: "session", projectId: "p", taskId: "t", sessionId: "s", view: "files", path: "a.go" });
  assert.equal(permalink(s, "3f9a2c1abc"), "/projects/p/tasks/t/sessions/s/code/files/a.go?commit=3f9a2c1abc");
  assert.equal(repositoryHref("p", "r", "agent/x"), "/projects/p/repositories/r/code?ref=agent%2Fx");
});

test("symlinks: inside the root opens the target; leaving it never does", () => {
  assert.equal(resolveTarget("web/vendor/alpine.js", "../../node_modules/alpinejs/dist/module.esm.js"), "node_modules/alpinejs/dist/module.esm.js");
  assert.equal(resolveTarget("docs/link", "./guide.md"), "docs/guide.md");
  assert.equal(resolveTarget("web/link", "../../../etc/passwd"), null);
  assert.equal(resolveTarget("link", "/etc/passwd"), null);
  assert.equal(resolveTarget("link", ".git/config"), null);
});

const MAP = {
  source: {},
  contexts: [
    { id: "c1", name: "Architecture", purpose: "Zones", fileCount: 10, zones: [{ id: "z1", name: "arch", pattern: "internal/application/architecture/**", fileCount: 10 }] },
    { id: "c2", name: "Workspaces", purpose: "Git", fileCount: 40, zones: [{ id: "z2", name: "git", pattern: "internal/adapter/out/git/**", fileCount: 40 }, { id: "z3", name: "empty", pattern: "nothing/*.go", fileCount: 0 }] },
  ],
  unassignedZones: [{ id: "z9", name: "loose", pattern: "*.md", fileCount: 5 }],
  unzonedFileCount: 212,
  fileCount: 267,
};

test("zones: a context's hue is its index in the map, a loose zone has none", () => {
  const index = zoneIndex(MAP);
  assert.equal(index.get("z1").hue, "0");
  assert.equal(index.get("z3").hue, "1");
  assert.equal(index.get("z9").hue, "none");
  const { tags, more } = zoneTags(["z1", "z2", "z9", "unknown"], index);
  assert.deepEqual(tags.map((t) => t.name), ["arch", "git"]);
  assert.deepEqual(more, { label: "+1", title: "loose" });
  assert.equal(tags[0].title, "Zone · Architecture context");
  assert.deepEqual(zoneDot(["z2"], index), { hue: "1", label: "git, Workspaces" });
  assert.equal(zoneDot([], index), null);
});

test("a zone link opens the longest literal directory of its pattern", () => {
  assert.equal(literalDir("internal/adapter/out/git/**"), "internal/adapter/out/git");
  assert.equal(literalDir("web/src/*.js"), "web/src");
  assert.equal(literalDir("**/*.go"), "");
  assert.equal(literalDir("docs/README.md"), "docs");
  assert.equal(literalDir("^internal/"), "internal", "a regular expression");
  assert.equal(literalDir("^internal/adapter/out/git/.*\\.go$"), "internal/adapter/out/git");
  assert.equal(literalDir("^web\\/src\\/"), "web/src");
});

test("the map's cards: contexts in order, then no context, then unzoned", () => {
  const v = mapView(MAP);
  assert.equal(v.empty, false);
  assert.deepEqual(v.cards.map((c) => [c.name, c.hue, c.kind, c.count]), [
    ["Architecture", "0", "", "10 files"],
    ["Workspaces", "1", "", "40 files"],
    ["No bounded context", "none", "unassigned", "5 files"],
    ["Unzoned files", "none", "unzoned", "212"],
  ]);
  assert.deepEqual(v.cards[1].zones.map((z) => [z.name, z.count, z.empty, z.share]), [["git", "40", false, 1], ["empty", "no files", true, 0]]);
  assert.deepEqual(v.spread.map((s) => [s.hue, s.kind, s.share]), [["0", "", 10], ["1", "", 40], ["none", "", 5], ["none", "unzoned", 212]]);
  assert.equal(v.totals, "267 files · 2 contexts · 4 zones · 212 unzoned");
  assert.equal(mapView({ ...MAP, contexts: [], unassignedZones: [] }).empty, true);
});
