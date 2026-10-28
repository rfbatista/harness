import { assert, file, test } from "../../../shared/testing/test.js";
import { makeWorld } from "../testing/fixtures.js";
import { memoryCode } from "../infrastructure/memory-gateway.js";
import { changesList, headText, mergeFiles } from "./changes.js";

file("code/presentation/changes");

/** A worktree with tracked changes in two directories (one staged), and three untracked files. */
function setup() {
  const world = makeWorld();
  const ws = world.workspaces.ws1;
  ws.staged = ["internal/feed/feed.go"];
  const memory = memoryCode(world);
  memory.change("ws1", { "web/new.js": "x\n", "docs/notes.md": "# n\n", "assets/new.png": { binary: 100 }, "internal/code/browser.go": null }, { untracked: ["web/new.js", "docs/notes.md", "assets/new.png"] });
  memory.change("ws1", { "internal/feed/feed.go": "package feed\n\nfunc New() {}\n" });
  return memory;
}

const read = async (memory, q = {}) => {
  const out = [];
  let after;
  do {
    const page = await memory.gateway.diff({ workspaceId: "ws1", scope: "working", limit: 2, after, hunks: "none", ...q });
    out.push(page);
    after = page.next ?? undefined;
  } while (after);
  return out;
};

const model = (pages, opts = {}) => {
  const last = pages.at(-1);
  return changesList({ files: mergeFiles(pages.map((p) => p.files)), totals: last, scope: "working", filter: [], ...opts });
};
const paths = (list) => list.sections.map((s) => s.groups.map((g) => [g.dir, g.rows.map((r) => r.name)]));

test("rows group by directory in path order, the directory named once and the file name on the row", async () => {
  const pages = await read(setup(), { scope: "all" });
  const list = changesList({ files: mergeFiles(pages.map((p) => p.files)), totals: pages.at(-1), scope: "all", filter: [] });
  assert.equal(list.sections.length, 1);
  assert.equal(list.sections[0].title, "");
  assert.deepEqual(paths(list)[0].map(([dir]) => dir), ["", "assets/", "docs/", "internal/feed/", "notes/", "web/"]);
  assert.deepEqual(list.sections[0].groups.find((g) => g.dir === "web/").rows.map((r) => [r.path, r.name]), [["web/new.js", "new.js"]]);
  assert.ok(list.sections[0].groups.every((g) => g.count === g.rows.length));
});

test("the working scope has Tracked changes then Untracked, each with its count and its own groups", async () => {
  const pages = await read(setup());
  assert.ok(pages.length > 1, "more than one page");
  const list = model(pages);
  assert.deepEqual(list.sections.map((s) => [s.key, s.title, s.note, s.count]), [
    ["tracked", "Tracked changes", "vs HEAD", 3],
    ["untracked", "Untracked", "new files, not added", 4],
  ]);
  assert.deepEqual(paths(list), [
    [["", ["README.md"]], ["internal/code/", ["browser.go"]], ["internal/feed/", ["feed.go"]]],
    [["assets/", ["new.png"]], ["docs/", ["notes.md"]], ["notes/", ["todo.md"]], ["web/", ["new.js"]]],
  ]);
});

test("the staged flag is on rows with an index change, and an untracked row says new where a tracked one counts lines", async () => {
  const list = model(await read(setup()));
  const rows = Object.fromEntries(list.sections.flatMap((s) => s.groups.flatMap((g) => g.rows)).map((r) => [r.path, r]));
  assert.deepEqual([rows["internal/feed/feed.go"].staged, rows["internal/feed/feed.go"].flag], [true, "staged"]);
  assert.deepEqual([rows["README.md"].staged, rows["README.md"].flag], [false, ""]);
  assert.deepEqual([rows["web/new.js"].note, rows["web/new.js"].stat], ["new", null]);
  assert.equal(rows["assets/new.png"].note, "binary");
  assert.equal(rows["web/new.js"].mark.letter, "U");
  assert.ok(/untracked, new/.test(rows["web/new.js"].label));
  assert.ok(/staged/.test(rows["internal/feed/feed.go"].label));
  assert.equal(rows["internal/feed/feed.go"].note, "");
  assert.ok(rows["internal/feed/feed.go"].stat.added.startsWith("+"));
});

test("the head reads N files · +A −D · U new files in the working scope, without the new part elsewhere", async () => {
  const memory = setup();
  const pages = await read(memory);
  const total = pages.at(-1);
  assert.equal(headText(total, "working"), `7 files · +${total.added} −${total.deleted} · 4 new files`);
  assert.equal(model(pages).head.text, headText(total, "working"));
  assert.ok(/^1 file · \+3 −0 · 1 new file$/.test(headText({ fileCount: 1, added: 3, deleted: 0, sections: { tracked: 0, untracked: 1, staged: 0 } }, "working")));
  assert.equal(headText({ fileCount: 2, added: 10, deleted: 1_200, sections: { tracked: 2, untracked: 0, staged: 0 } }, "working"), "2 files · +10 −1,200");
  assert.equal(headText({ fileCount: 7, added: 4, deleted: 2, sections: null }, "branch"), "7 files · +4 −2");
});

test("the legend counts each status over the whole diff, across pages, in M A D R C T U order, and shows only what is there", async () => {
  const pages = await read(setup());
  assert.ok(pages[0].files.length < 7, "the first page holds a part");
  const first = changesList({ files: mergeFiles([pages[0].files]), totals: pages[0], scope: "working", filter: [] });
  const all = model(pages);
  const letters = (l) => l.legend.map((b) => [b.letter, b.count]);
  assert.deepEqual(letters(first), letters(all), "page one's counts are the whole diff's");
  assert.deepEqual(letters(all), [["M", 2], ["D", 1], ["U", 4]]);
  assert.deepEqual(all.legend.map((b) => [b.status, b.pressed, b.label]), [["modified", false, "2 modified"], ["deleted", false, "1 deleted"], ["untracked", false, "4 untracked"]]);
});

test("pressing a legend status hides the rows of the others, drops empty groups and sections, and restores them, reading no page", async () => {
  const memory = setup();
  const pages = await read(memory);
  const reads = memory.calls.length;
  const files = mergeFiles(pages.map((p) => p.files));
  const apply = (filter) => changesList({ files, totals: pages.at(-1), scope: "working", filter });
  const none = apply([]);
  const onlyU = apply(["untracked"]);
  assert.deepEqual(onlyU.sections.map((s) => s.key), ["untracked"]);
  assert.equal(onlyU.sections[0].count, 4);
  assert.deepEqual(onlyU.legend.map((b) => [b.letter, b.count, b.pressed]), [["M", 2, false], ["D", 1, false], ["U", 4, true]], "counts stay the whole diff's");
  const two = apply(["deleted", "modified"]);
  assert.deepEqual(two.sections.map((s) => [s.key, s.count]), [["tracked", 3]]);
  assert.deepEqual(two.sections[0].groups.map((g) => g.count), [1, 1, 1]);
  assert.deepEqual(apply([]), none, "unpressing restores the same list");
  assert.equal(memory.calls.length, reads, "no page was read");
  assert.equal(onlyU.empty, false);
  assert.equal(apply(["renamed"]).empty, true);
});

test("a file is one row by its path whichever page or section it came from, the latest read winning", async () => {
  const memory = setup();
  const pages = await read(memory);
  const seen = mergeFiles(pages.map((p) => p.files));
  assert.equal(seen.length, 7);
  // The same pages read again after notes/todo.md was added to the index and committed state moved: an older and a newer read of one path.
  const older = { ...seen.find((f) => f.path === "notes/todo.md") };
  const newer = { ...older, status: "added", section: "tracked", staged: true };
  const merged = mergeFiles([[older], [newer]]);
  assert.deepEqual(merged.map((f) => [f.path, f.section, f.staged]), [["notes/todo.md", "tracked", true]]);
  const list = changesList({ files: mergeFiles([seen, [newer]]), totals: pages.at(-1), scope: "working", filter: [] });
  const rows = list.sections.flatMap((s) => s.groups.flatMap((g) => g.rows)).filter((r) => r.path === "notes/todo.md");
  assert.equal(rows.length, 1);
  assert.equal(list.sections.find((s) => s.key === "tracked").groups.some((g) => g.dir === "notes/"), true);
  assert.equal(list.sections.find((s) => s.key === "untracked").groups.some((g) => g.dir === "notes/"), false);
  assert.equal(rows[0].key, "notes/todo.md");
});

test("rows of a list that moved keep their slug anchors unique and in path order", async () => {
  const list = model(await read(setup()));
  const rows = list.sections.flatMap((s) => s.groups.flatMap((g) => g.rows));
  assert.equal(new Set(rows.map((r) => r.slug)).size, rows.length);
  assert.ok(rows.every((r) => r.href === `#diff-${r.slug}`));
});

test("the head keeps the whole diff's totals and, with a legend status pressed, ends with a note naming the pressed words", async () => {
  const pages = await read(setup());
  const files = mergeFiles(pages.map((p) => p.files));
  const apply = (filter) => changesList({ files, totals: pages.at(-1), scope: "working", filter });
  const none = apply([]);
  assert.equal(none.head.note, "");
  const one = apply(["untracked"]);
  assert.equal(one.head.text, none.head.text, "the totals stay the whole diff's");
  assert.equal(one.head.note, "· showing untracked");
  assert.equal(apply(["deleted", "modified"]).head.note, "· showing modified and deleted", "in the legend's order");
  assert.equal(apply(["untracked", "deleted", "modified"]).head.note, "· showing modified, deleted and untracked");
});

test("rows carry the current file, the fresh files and the slugs the diff sections use", async () => {
  const pages = await read(setup());
  const files = mergeFiles(pages.map((p) => p.files));
  const slugMap = new Map(files.map((f, i) => [f.path, `s${i}`]));
  const list = changesList({ files, totals: pages.at(-1), scope: "working", filter: [], slugMap, current: "README.md", fresh: ["web/new.js"] });
  const rows = Object.fromEntries(list.sections.flatMap((s) => s.groups.flatMap((g) => g.rows)).map((r) => [r.path, r]));
  assert.equal(rows["README.md"].current, "location");
  assert.equal(rows["web/new.js"].current, null);
  assert.equal(rows["web/new.js"].fresh, "");
  assert.equal(rows["README.md"].fresh, null);
  assert.equal(rows["README.md"].href, `#diff-${slugMap.get("README.md")}`);
});

test("a row is the same object while its file, slug, current and fresh state did not change (a selection redoes two)", async () => {
  const pages = await read(setup());
  const files = mergeFiles(pages.map((p) => p.files));
  const cache = new Map();
  const make = (current) => changesList({ files, totals: pages.at(-1), scope: "working", filter: [], current, cache });
  const rowsOf = (l) => Object.fromEntries(l.sections.flatMap((s) => s.groups.flatMap((g) => g.rows)).map((r) => [r.path, r]));
  const a = rowsOf(make("README.md"));
  const b = rowsOf(make("web/new.js"));
  const same = Object.keys(a).filter((p) => a[p] === b[p]);
  assert.equal(same.length, Object.keys(a).length - 2);
});

test("without the server's counts the legend and sections count the files read", async () => {
  const pages = await read(setup(), { scope: "working", limit: 50 });
  const total = { ...pages.at(-1), counts: { modified: 0, added: 0, deleted: 0, renamed: 0, copied: 0, type_changed: 0, untracked: 0 }, sections: null };
  const list = changesList({ files: mergeFiles(pages.map((p) => p.files)), totals: total, scope: "working", filter: [] });
  assert.deepEqual(list.legend.map((b) => [b.letter, b.count]), [["M", 2], ["D", 1], ["U", 4]]);
  assert.deepEqual(list.sections.map((s) => [s.key, s.count]), [["tracked", 3], ["untracked", 4]]);
  assert.equal(list.head.text, `7 files · +${total.added} −${total.deleted} · 4 new files`);
});

test("a list can link its rows elsewhere than the diff's anchors", async () => {
  const pages = await read(setup());
  const files = mergeFiles(pages.map((p) => p.files));
  const list = changesList({ files, totals: pages.at(-1), scope: "working", filter: [], href: (slug) => `/code/changes?scope=working#diff-${slug}` });
  const rows = list.sections.flatMap((s) => s.groups.flatMap((g) => g.rows));
  assert.ok(rows.every((r) => r.href === `/code/changes?scope=working#diff-${r.slug}`));
});

test("the files at the top level sit under a head named ./ and the other groups under their directory", async () => {
  const list = model(await read(setup()));
  assert.deepEqual(list.sections[0].groups.map((g) => g.label), ["./", "internal/code/", "internal/feed/"]);
});
