import { Codes } from "../../../../shared/domain/errors.js";
import { mount } from "../../../../shared/testing/alpine.js";
import { flush } from "../../../../shared/testing/doubles.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { memoryCode } from "../../infrastructure/memory-gateway.js";
import { plainHighlighter } from "../../infrastructure/plain-highlighter.js";
import { makeWorld } from "../../testing/fixtures.js";
import { finderRows } from "./codeBrowser.js";
import { codePanel } from "./codePanel.js";

file("code/presentation/codePanel");

const memoryPrefs = () => {
  const m = new Map();
  return { get: (k) => m.get(k) ?? null, set: (k, v) => (v === null ? m.delete(k) : m.set(k, v)) };
};

/** A session's Code panel over the memory world, no seed painted; idle runs at once. */
async function setup({ world = makeWorld(), live = true, shown = true } = {}) {
  const memory = memoryCode(world);
  const pushed = [];
  const history = { pushState: (_s, _t, url) => pushed.push(url), replaceState: (_s, _t, url) => pushed.push(url) };
  const deps = { gateway: memory.gateway, highlighter: plainHighlighter(), prefs: memoryPrefs(), history, location: { pathname: "/", search: "", hash: "" }, idle: (cb) => cb() };
  const el = document.createElement("section");
  const mounted = mount(
    () => codePanel(deps)({
      key: "s1", projectId: "p1", taskId: "t1", sessionId: "s1", workspaceId: "ws1", repositoryId: "r1",
      repositoryName: "harness", branch: "agent/visualize-code", baseRef: "main", live, shown, seedId: "",
    }),
    { el },
  );
  mounted.instance.init();
  await flush();
  await flush();
  return { ...mounted, panel: mounted.instance, memory, pushed };
}

test("the panel reads the diff for the badge, and the uncommitted count, without being asked", async () => {
  const { panel, dispatched } = await setup({ shown: false });
  assert.equal(panel.diff.files.length, 5);
  assert.deepEqual(dispatched.find((d) => d.name === "code-files-counted").detail, { sessionId: "s1", count: 5 });
  assert.equal(panel.dirty, 2);
  assert.deepEqual([panel.bar.state, panel.bar.word, panel.bar.dirty], ["live", "live", "2 uncommitted"]);
  assert.equal(panel.view, "changes");
  assert.deepEqual(panel.views.map((v) => [v.label, v.count]), [["Changes", "5"], ["Files", ""], ["Map", ""]]);
});

test("the changed files: marks, counts, a deleted file over 50 lines starts collapsed", async () => {
  const { panel } = await setup();
  const rows = Object.fromEntries(panel.summaryRows.map((r) => [r.path, r]));
  assert.deepEqual([rows["docs/old.md"].mark.letter, rows["notes/todo.md"].mark.letter, rows["README.md"].mark.letter], ["D", "U", "M"]);
  assert.deepEqual(rows["internal/feed/feed.go"].stat, { added: "+1", deleted: "−0" });
  assert.equal(panel.diffFiles.find((f) => f.path === "docs/old.md").collapsed, "");
  assert.deepEqual(panel.totals, { files: "5 files", added: panel.totals.added, deleted: panel.totals.deleted });
});

test("a scope change reads that scope and pushes its URL", async () => {
  const { panel, pushed } = await setup();
  panel.clickScope({ preventDefault() {}, currentTarget: { getAttribute: () => null } }, "working");
  await flush();
  assert.equal(panel.scope, "working");
  assert.deepEqual(panel.diff.files.map((f) => f.path), ["README.md", "notes/todo.md"]);
  assert.ok(pushed.at(-1).endsWith("/code/changes?scope=working"));
});

test("a change while the reader is at the top re-reads in place and stays live", async () => {
  const { panel, memory } = await setup();
  memory.change("ws1", { "web/new.js": "x\n" }, { untracked: ["web/new.js"] });
  await flush();
  await flush();
  assert.equal(panel.liveState, "live");
  assert.ok(panel.diff.files.some((f) => f.path === "web/new.js"));
  assert.ok(panel.freshPaths.includes("web/new.js"));
});

test("a change after the reader scrolled holds the diff, lists the new file, and says so once; R applies it", async () => {
  const { panel, memory, dispatched } = await setup();
  panel.touched = true;
  memory.change("ws1", { "web/new.js": "x\n" }, { untracked: ["web/new.js"] });
  await flush();
  await flush();
  memory.change("ws1", { "web/other.js": "y\n" }, { untracked: ["web/other.js"] });
  await flush();
  await flush();
  assert.equal(panel.liveState, "stale");
  assert.equal(panel.bar.word, "Changed on disk");
  assert.ok(!panel.diff.files.some((f) => f.path === "web/new.js"), "the open diff does not move");
  assert.ok(panel.summaryRows.some((r) => r.path === "web/other.js"), "the list follows");
  assert.equal(dispatched.filter((d) => d.name === "rail-announce").length, 1);
  await panel.reload();
  assert.equal(panel.liveState, "live");
  assert.ok(panel.diff.files.some((f) => f.path === "web/other.js"));
});

test("a gone worktree: Branch, with All and Working disabled and why", async () => {
  const world = makeWorld();
  const { panel } = await setup({ world, live: true });
  panel.diff = null;
  world.workspaces.ws1.gone = true;
  await panel.showDiff();
  assert.deepEqual([panel.gone, panel.liveState, panel.scope], [true, "gone", "branch"]);
  assert.deepEqual(panel.scopes.map((s) => [s.label, s.disabled]), [["All", "true"], ["Branch", null], ["Working", "true"]]);
  panel.showView("files");
  assert.equal(panel.goneEmpty, true);
  assert.equal(panel.browseBranchHref, "/projects/p1/repositories/r1/code?ref=agent%2Fvisualize-code");
});

test("a gone base: Working, with All and Branch titled by the base's name", async () => {
  const world = makeWorld();
  const memory = memoryCode(world);
  memory.baseGone("ws1");
  const { panel } = await setup({ world });
  assert.deepEqual([panel.baseGone, panel.scope], [true, "working"]);
  assert.equal(panel.scopes[0].title, "The base branch main no longer exists");
  assert.equal(panel.baseGoneLine, "The base branch main no longer resolves");
});

test("Files: a file opens with its crumbs, meta and zones; a deleted one opens its diff", async () => {
  const { panel, memory } = await setup();
  panel.showView("files");
  await flush();
  await panel.openPath("internal/feed/feed.go");
  assert.equal(panel.file.path, "internal/feed/feed.go");
  assert.deepEqual(panel.fileCrumbs.map((c) => c.label), ["harness", "internal", "feed", "feed.go"]);
  assert.equal(panel.fileMeta, "Go · 9 lines · 120 B");
  assert.deepEqual(panel.fileZoneTags.tags.map((z) => [z.name, z.hue]), [["feed", "1"]]);
  const before = memory.calls.filter((c) => c.name === "file").length;
  await panel.openPath("docs/old.md");
  assert.equal(panel.view, "changes");
  assert.equal(memory.calls.filter((c) => c.name === "file").length, before, "no /file for a deleted file");
});

test("symlinks and submodules open a placeholder, never /file", async () => {
  const { panel, memory } = await setup({ live: false });
  panel.showView("files");
  await flush();
  await panel.openPath("web/link.js");
  assert.deepEqual([panel.placeholder.title, panel.placeholder.action], ["Symbolic link", "Open the target"]);
  await panel.openPath("web/out");
  assert.equal(panel.placeholder.text, "Points outside this repository, so it is not shown.");
  await panel.openPath("third_party/xterm");
  assert.equal(panel.placeholder.title, "Submodule, not read into");
  assert.equal(memory.calls.filter((c) => c.name === "file").length, 0);
});

test("Go to line clamps to the file and says so", async () => {
  const { panel, dispatched } = await setup();
  panel.showView("files");
  await panel.openPath("README.md");
  panel.openGoToLine();
  panel.goToLine("120");
  assert.deepEqual(panel.target, { start: 3, end: 3 });
  assert.equal(dispatched.filter((d) => d.name === "rail-announce").at(-1).detail, "The file has 3 lines; went to line 3.");
  panel.paletteMode = "line";
  panel.goToLine("abc");
  assert.equal(panel.paletteNote, "Type a line, like 120, or a range, like 120-140.");
});

test("Load diff asks for one withheld file with path= and swaps its hunks in", async () => {
  const { panel } = await setup({ world: makeWorld({ limits: { perFile: 1 } }) });
  const f = panel.diffFiles.find((x) => x.path === "README.md");
  assert.equal(f.withheld.action, "load");
  await panel.loadWithheld("README.md");
  assert.ok(panel.diff.files.find((x) => x.path === "README.md").hunks.length > 0);
});

test("errors from a read show their code and next step", async () => {
  const { panel, memory } = await setup();
  panel.showView("files");
  memory.failNext(Codes.PATH_NOT_FOUND, "gone", 404);
  await panel.loadFile("nope.go");
  assert.equal(panel.problemLine, "PATH_NOT_FOUND · nope.go is not on agent/visualize-code");
  assert.equal(panel.problem.action, "Show the tree root");
});

test("Find file: name then faint directory, matches marked, the change mark beside", () => {
  const diff = { files: [{ path: "internal/feed/feed.go", status: "modified" }] };
  const [row] = finderRows(["internal/feed/feed.go", "web/app.js"], "feedgo", 0, diff);
  assert.equal(row.path, "internal/feed/feed.go");
  assert.deepEqual(row.nameParts.map((p) => [p.text, p.mark]), [["feed", true], [".", false], ["go", true]]);
  assert.equal(row.dirParts.map((p) => p.text).join(""), "internal/feed");
  assert.equal(row.mark.letter, "M");
});

test("keys: G then F switches view; ] steps through the diff's files; typing in a field is left alone", async () => {
  const { panel } = await setup();
  const key = (k, target = document.createElement("div")) => {
    const e = { key: k, target, defaultPrevented: false, preventDefault() { this.defaultPrevented = true; } };
    panel.onKey(e);
    return e;
  };
  panel.currentFile = panel.diff.files[0].path;
  key("]");
  key("g");
  key("f");
  assert.equal(panel.view, "files");
  const typed = key("t", document.createElement("input"));
  assert.equal(typed.defaultPrevented, false);
});
