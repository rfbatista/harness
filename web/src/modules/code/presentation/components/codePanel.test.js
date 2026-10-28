import { Codes } from "../../../../shared/domain/errors.js";
import { mount } from "../../../../shared/testing/alpine.js";
import { flush } from "../../../../shared/testing/doubles.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { memoryCode } from "../../infrastructure/memory-gateway.js";
import { inlinePreparer } from "../../infrastructure/preparer.js";
import { plainHighlighter } from "../../infrastructure/plain-highlighter.js";
import { makeWorld, manyChangedFilesWorld } from "../../testing/fixtures.js";
import { finderRows } from "./codeBrowser.js";
import { codePanel } from "./codePanel.js";

file("code/presentation/codePanel");

const memoryPrefs = () => {
  const m = new Map();
  return { get: (k) => m.get(k) ?? null, set: (k, v) => (v === null ? m.delete(k) : m.set(k, v)) };
};

/** A session's Code panel over the memory world, no seed painted; idle runs at once. */
async function setup({ world = makeWorld(), live = true, shown = true, prefs = memoryPrefs(), seedId = "", slow = 0, settle = true } = {}) {
  const memory = memoryCode(world);
  if (slow) memory.slow(slow);
  const pushed = [];
  const history = { pushState: (_s, _t, url) => pushed.push(url), replaceState: (_s, _t, url) => pushed.push(url) };
  const deps = { gateway: memory.gateway, highlighter: plainHighlighter(), preparer: inlinePreparer(plainHighlighter()), prefs, history, location: { pathname: "/", search: "", hash: "" }, idle: (cb) => cb() };
  const el = document.createElement("section");
  const mounted = mount(
    () => codePanel(deps)({
      key: "s1", projectId: "p1", taskId: "t1", sessionId: "s1", workspaceId: "ws1", repositoryId: "r1",
      repositoryName: "harness", branch: "agent/visualize-code", baseRef: "main", live, shown, seedId,
    }),
    { el },
  );
  mounted.instance.init();
  if (settle) {
    await flush();
    await flush();
  }
  return { ...mounted, panel: mounted.instance, memory, pushed, prefs };
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
  const loaded = panel.diff.files.find((x) => x.path === "README.md");
  assert.deepEqual([loaded.hasHunks, loaded.withheld], [true, null]);
  assert.equal(panel.diffFiles.find((x) => x.path === "README.md").hasLines, true);
  assert.equal(panel.diffFiles.find((x) => x.path === "docs/old.md").hasLines, false, "the other files stay withheld");
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

test("Unified | Side by side is remembered; side by side is the default, also for a bad stored value (ADR 0037)", async () => {
  const first = await setup();
  assert.deepEqual([first.panel.unifiedPressed, first.panel.splitPressed], ["false", "true"]);
  first.panel.setLayout("unified");
  assert.equal(first.prefs.get("code.layout"), "unified");
  const again = await setup({ prefs: first.prefs });
  assert.deepEqual([again.panel.unifiedPressed, again.panel.splitPressed], ["true", "false"]);
  const bad = await setup({ prefs: { get: () => "banana", set() {} } });
  assert.equal(bad.panel.layout, "split");
});

test("Side by side is disabled below 44rem, with its reason, and cannot be chosen", async () => {
  const { panel } = await setup();
  assert.deepEqual([panel.canSplit, panel.sideBySideDisabled, panel.sideBySideTitle], [true, null, null]);
  panel.narrow = true;
  assert.deepEqual([panel.canSplit, panel.sideBySideDisabled, panel.sideBySideTitle], [false, "true", "Side by side needs a wider window"]);
  panel.setLayout("unified");
  panel.setLayout("split");
  assert.equal(panel.layout, "unified");
});

test("an empty diff names the base it was compared with", async () => {
  const { panel } = await setup();
  panel.diff = { ...panel.diff, files: [] };
  assert.equal(panel.noChangesTitle, "No changes against main yet");
  panel.route = { ...panel.route, scope: "working" };
  assert.equal(panel.noChangesTitle, "No uncommitted changes");
});

const keyEvent = (key) => ({ key, preventDefault() {}, stopPropagation() {} });

test("the split: the default reads as percent, arrows and Home move it, and the width is remembered", async () => {
  const first = await setup();
  const { panel } = first;
  panel.measure(1000);
  assert.deepEqual([panel.splitNow, panel.splitMin, panel.splitMax, panel.splitStyle], [29, 19, 50, null]);
  panel.splitKey(keyEvent("ArrowRight"));
  assert.deepEqual([panel.splitNow, panel.splitStyle], [31, "--code-side: 30.8%"]);
  assert.equal(first.prefs.get("code.split"), "30.8");
  const again = await setup({ prefs: first.prefs });
  again.panel.measure(1000);
  assert.equal(again.panel.splitNow, 31, "a reload keeps it");
  again.panel.splitKey(keyEvent("Home"));
  assert.deepEqual([again.panel.splitNow, again.panel.splitStyle, first.prefs.get("code.split")], [29, null, null]);
});

test("a stored split that is not a width is ignored", async () => {
  const { panel } = await setup({ prefs: { get: (k) => (k === "code.split" ? "wide" : null), set() {} } });
  panel.measure(1000);
  assert.equal(panel.splitStyle, null);
});

test("dragging the separator follows the pointer inside the bounds and stores once, on release", async () => {
  const { panel, prefs } = await setup();
  panel.measure(1000);
  panel.$refs = { layout: { getBoundingClientRect: () => ({ left: 100, width: 1000 }) } };
  const target = { setPointerCapture() {}, releasePointerCapture() {} };
  panel.startSplit({ pointerId: 1, target, preventDefault() {} });
  panel.moveSplit({ pointerId: 1, clientX: 100 + 400 });
  assert.equal(panel.splitNow, 40);
  panel.moveSplit({ pointerId: 1, clientX: 100 + 900 });
  assert.equal(panel.splitNow, 50, "never past half");
  panel.moveSplit({ pointerId: 1, clientX: 100 + 20 });
  assert.equal(panel.splitNow, 19, "never under 12rem");
  assert.equal(prefs.get("code.split"), null, "nothing stored while dragging");
  panel.endSplit({ pointerId: 1, target });
  assert.equal(prefs.get("code.split"), "19.2");
  panel.moveSplit({ pointerId: 1, clientX: 100 + 400 });
  assert.equal(panel.splitNow, 19, "a move after release is not a drag");
});

test("a double-click resets the split to the default", async () => {
  const { panel, prefs } = await setup();
  panel.measure(1000);
  panel.splitKey(keyEvent("ArrowRight"));
  panel.resetSplit();
  assert.deepEqual([panel.splitStyle, prefs.get("code.split")], [null, null]);
});

const shiftKey = (key, target = document.createElement("div")) => ({ key, shiftKey: true, target, defaultPrevented: false, preventDefault() { this.defaultPrevented = true; } });

test("⇧F enters and leaves full screen, tells the page, and is never remembered", async () => {
  const stored = [];
  const { panel, dispatched, tick } = await setup({ prefs: { get: () => null, set: (k, v) => stored.push([k, v]) } });
  const focused = [];
  const button = Object.assign(document.createElement("button"), { focus: () => focused.push("button") });
  button.setAttribute("data-code-fullscreen", "");
  panel.$root.append(button);
  panel.$refs = { main: { focus: () => focused.push("diff") } };
  const events = () => dispatched.filter((d) => d.name === "code-fullscreen").map((d) => d.detail.on);

  panel.onKey(shiftKey("F"));
  tick();
  await flush();
  assert.deepEqual([panel.fullscreen, panel.fullscreenPressed, panel.fullscreenAttr, events(), focused], [true, "true", "", [true], ["diff"]]);
  panel.onKey(shiftKey("F"));
  tick();
  await flush();
  assert.deepEqual([panel.fullscreen, panel.fullscreenAttr, events(), focused], [false, null, [true, false], ["diff", "button"]]);
  assert.deepEqual(stored.filter(([k]) => /full/i.test(k)), [], "nothing about full screen is stored");
});

test("Esc is not the full-screen key", async () => {
  const { panel } = await setup();
  panel.toggleFullscreen();
  panel.onKey({ key: "Escape", target: document.createElement("div"), preventDefault() {} });
  assert.equal(panel.fullscreen, true);
});

test("⇧E in full screen hides the file list, and does not touch the session list", async () => {
  const { panel, dispatched } = await setup();
  panel.toggleFullscreen();
  panel.onKey(shiftKey("E"));
  assert.deepEqual([panel.listHidden, panel.listAttr, dispatched.some((d) => d.name === "code-expand")], [true, "hidden", false]);
  panel.onKey(shiftKey("E"));
  assert.deepEqual([panel.listHidden, panel.listAttr], [false, null]);
});

test("the full-screen bar says the session, branch to base, and the summary", async () => {
  const { panel } = await setup();
  assert.deepEqual([panel.branch, panel.baseRef, panel.title], ["agent/visualize-code", "main", ""]);
  assert.equal(panel.totals.files, "5 files");
});

test("the Code tab leaving the front leaves full screen", async () => {
  const { panel, dispatched } = await setup();
  panel.toggleFullscreen();
  panel.tabShown({ detail: { sessionId: "s1", shown: false } });
  assert.equal(panel.fullscreen, false);
  assert.deepEqual(dispatched.filter((d) => d.name === "code-fullscreen").map((d) => d.detail.on), [true, false]);
});

test("a diff of 120 changed files lists the first page and heads it with the whole diff's totals", async () => {
  const { panel, dispatched } = await setup({ world: manyChangedFilesWorld(120) });
  assert.equal(panel.diff.files.length, 50);
  assert.equal(panel.summaryRows.length, 50);
  assert.equal(panel.totals.files, "125 files");
  assert.ok(panel.totals.added !== "+0");
  assert.deepEqual(panel.views.map((v) => v.count), ["125", "", ""]);
  assert.deepEqual(dispatched.find((d) => d.name === "code-files-counted").detail, { sessionId: "s1", count: 125 });
  assert.equal(panel.dirty, 122, "the uncommitted count is the whole working diff's");
});

test("a first paint's page is used as read: the panel asks for no first page, only the uncommitted totals", async () => {
  const world = manyChangedFilesWorld(120);
  const memory = memoryCode(world);
  const seed = document.createElement("script");
  seed.id = "seed-s1";
  seed.type = "application/json";
  seed.textContent = JSON.stringify({
    project_id: "p1",
    route: { kind: "session", project_id: "p1", task_id: "t1", session_id: "s1", view: "changes" },
    source: { workspace_id: "ws1" },
    repository_name: "harness",
    diff: memory.wire.diff({ workspaceId: "ws1", scope: "all", limit: 50 }),
  });
  document.body.append(seed);
  try {
    const { panel, memory: m } = await setup({ world, seedId: "seed-s1" });
    assert.equal(panel.diff.files.length, 50);
    assert.equal(panel.totals.files, "125 files");
    assert.deepEqual(m.calls.filter((c) => c.name === "diff").map((c) => c.args.q.limit), [0], "one totals read for the count in the bar");
  } finally {
    seed.remove();
  }
});

test("while a diff loads the bar's status spins and the changed files list skeleton rows, both aria-busy; neither stays", async () => {
  const { panel } = await setup({ slow: 40, settle: false });
  await flush();
  assert.deepEqual([panel.busyAttr, panel.stateText, panel.listBusy], ["", "Loading changes", "true"]);
  assert.equal(panel.listSkeleton.length, 3);
  assert.equal(panel.summaryRows.length, 0, "no file yet");
  await new Promise((r) => setTimeout(r, 150));
  assert.deepEqual([panel.busyAttr, panel.listBusy, panel.listSkeleton.length], [null, null, 0]);
  assert.equal(panel.hasState || panel.busyAttr !== null, true, "the live word is back");
  assert.equal(panel.stateText, "live");
});

const scrollerOf = (el, { top = 0, height = 800, content = 10_000 } = {}) => {
  Object.defineProperties(el, {
    scrollTop: { value: top, writable: true },
    clientHeight: { value: height },
    scrollHeight: { value: content },
  });
  return el;
};

test("Show more reads the next page with the held cursor, the count left falls, and the foot goes at the last page", async () => {
  const { panel, memory } = await setup({ world: manyChangedFilesWorld(120) });
  assert.deepEqual([panel.moreShown, panel.moreCount], [true, "75"]);
  await panel.loadMore();
  assert.deepEqual([panel.diff.files.length, panel.moreCount, panel.totals.files], [100, "25", "125 files"]);
  const pages = () => memory.calls.filter((c) => c.name === "diff" && c.args.q.after);
  assert.deepEqual(pages().map((c) => c.args.q.after), [panel.diff.files[49].path]);
  await panel.loadMore();
  assert.deepEqual([panel.diff.files.length, panel.moreShown, panel.diff.next], [125, false, null]);
  await panel.loadMore();
  assert.equal(pages().length, 2, "no read past the last page");
  assert.equal(panel.summaryRows.length, 125);
  assert.equal(new Set(panel.diffFiles.map((f) => f.slug)).size, 125, "slugs stay unique across pages");
});

test("while a later page loads the list ends with skeleton rows; a click on Show more twice reads once", async () => {
  const { panel, memory } = await setup({ world: manyChangedFilesWorld(120) });
  memory.slow(30);
  const a = panel.loadMore();
  const b = panel.loadMore();
  await flush();
  assert.deepEqual([panel.listBusy, panel.listSkeleton.length], ["true", 3]);
  await Promise.all([a, b]);
  assert.equal(panel.diff.files.length, 100);
  assert.equal(panel.listBusy, null);
});

test("scrolling near the end of the loaded sections reads the next page once", async () => {
  const { panel, memory } = await setup({ world: manyChangedFilesWorld(120) });
  const main = scrollerOf(document.createElement("div"), { top: 9_000, height: 800, content: 10_000 });
  panel.mainScroller = () => main;
  const before = memory.calls.length;
  await Promise.all([panel.followScroll(), panel.followScroll()]);
  assert.equal(memory.calls.length - before, 1);
  assert.equal(panel.diff.files.length, 100);
  main.scrollTop = 100;
  await panel.followScroll();
  assert.equal(panel.diff.files.length, 100, "far from the end: no read");
});

test("a deep link to a file on a later page loads pages up to it and selects it", async () => {
  const { panel } = await setup({ world: manyChangedFilesWorld(120) });
  const path = "bulk/d0007/f0075.js";
  assert.ok(!panel.diff.files.some((f) => f.path === path));
  const slug = "diff-bulk-d0007-f0075-js";
  await panel.readHash(`#${slug}`);
  assert.equal(panel.currentFile, path);
  assert.ok(panel.diff.files.some((f) => f.path === path));
  assert.equal(panel.diff.files.length, 100, "only the pages up to it");
});

test("] on the last loaded file reads the next page and steps into it, instead of wrapping", async () => {
  const { panel } = await setup({ world: manyChangedFilesWorld(120) });
  panel.currentFile = panel.diff.files[49].path;
  await panel.stepFile(1);
  assert.equal(panel.diff.files.length, 100);
  assert.equal(panel.currentFile, panel.diff.files[50].path);
});

test("opening a deleted file of a later page from Files loads pages up to it and shows its diff", async () => {
  const world = manyChangedFilesWorld(120);
  world.repositories.r1.refs.main.files["zzz/deleted.md"] = "bye\n";
  world.workspaces.ws1.committed["zzz/deleted.md"] = "bye\n";
  world.workspaces.ws1.disk["zzz/deleted.md"] = "bye\n";
  delete world.workspaces.ws1.disk["zzz/deleted.md"];
  const { panel, tick } = await setup({ world });
  panel.showView("files");
  await flush();
  assert.ok(!panel.diff.files.some((f) => f.path === "zzz/deleted.md"));
  await panel.openPath("zzz/deleted.md");
  tick();
  assert.ok(panel.diff.files.some((f) => f.path === "zzz/deleted.md"));
  assert.equal(panel.view, "changes");
  assert.equal(panel.currentFile, "zzz/deleted.md");
});
