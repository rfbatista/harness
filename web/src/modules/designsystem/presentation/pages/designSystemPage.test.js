import { fixedClock } from "../../../../shared/infrastructure/clock.js";
import { StructuredError } from "../../../../shared/domain/errors.js";
import { FeedStatus } from "../../../../shared/domain/feed.js";
import { mount, seededElement } from "../../../../shared/testing/alpine.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { designSystemPage } from "./designSystemPage.js";

file("designsystem/presentation/pages/designSystemPage");

const colour = { group: "colors", name: "signal", type: "color", value: "#0a0", note: "" };
const state = (over = {}) => ({
  exists: true,
  version: 3,
  updatedAt: "2026-10-01T10:00:00Z",
  updatedBy: "person",
  record: { tokens: [colour], principles: "# Calm\n", components: [{ slug: "button" }] },
  ...over,
});
const empty = () => state({ exists: false, version: 0, updatedAt: "", updatedBy: "", record: { tokens: [], principles: "", components: [] } });

/** A gateway the test plays by hand: what the server holds, the calls made, and the feed. */
function fakeGateway(initial) {
  const g = {
    server: initial,
    saves: [],
    renames: [],
    refuse: null,
    afterSave: null,
    /** The version list the server holds, newest first, and the requests made for it. */
    list: [],
    asked: [],
    full: {},
    async versions(projectId, { limit, before } = {}) {
      g.asked.push({ limit, before });
      return g.list.filter((v) => !before || v.version < before).slice(0, limit);
    },
    async version(projectId, number) {
      const v = g.full[number];
      if (!v) throw new StructuredError("VERSION_NOT_FOUND", "no such version", 404);
      return structuredClone(v);
    },
    feed: null,
    fromSeed: (s) => s,
    async get() {
      return structuredClone(g.server);
    },
    async save(projectId, base, record, message) {
      g.saves.push({ projectId, base, record, message });
      if (g.refuse) throw g.refuse;
      g.server = { ...g.server, exists: true, version: base + 1, record };
      g.afterSave?.();
      return base + 1;
    },
    async rename(projectId, base, from, to) {
      g.renames.push({ base, from, to });
      if (g.refuse) throw g.refuse;
      g.server = { ...g.server, version: base + 1, record: { ...g.server.record, tokens: g.server.record.tokens.map((t) => (t.name === from.name ? { ...t, ...to } : t)) } };
      return base + 1;
    },
    follow(projectId, handlers) {
      g.feed = handlers;
      return () => (g.feed = null);
    },
  };
  return g;
}

function page(initial = state(), seed = {}, extra = {}) {
  const gateway = fakeGateway(initial);
  const el = seededElement({ project_id: "p1", base: "/projects/p1/design-system", tab: "", state: initial, ...seed });
  const pushed = [];
  const m = mount(designSystemPage({ gateway, clock: fixedClock("2026-10-01T10:30:00Z"), history: { pushState: (_s, _t, url) => pushed.push(url) }, location: { search: "" }, ...extra }), { el });
  m.instance.init();
  return { ...m, gateway, pushed, p: m.instance };
}

const changed = (current) => new StructuredError("DESIGN_SYSTEM_CHANGED", "changed", 409, { current_version: current });
const invalid = (errors) => new StructuredError("DESIGN_SYSTEM_INVALID", "invalid", 400, { errors });

test("the seeded state is on the page at once, with no read", () => {
  const { p, gateway } = page();
  assert.equal(p.loaded, true);
  assert.equal(p.rows.length, 1);
  assert.equal(p.version, 3);
  assert.equal(p.isOpen, true);
  assert.equal(p.dirty, false);
  assert.equal(p.versionText, "Saves as version 4");
  assert.equal(p.savedLine, "Version 3 · saved by you, 30m ago");
  assert.equal(typeof gateway.feed.onSaved, "function");
});

test("a project with no design system opens on the first-start screen, and Start blank stores nothing", () => {
  const { p, gateway } = page(empty());
  assert.equal(p.isFirstStart, true);
  assert.equal(p.isOpen, false);
  p.startBlank();
  assert.equal(p.isFirstStart, false);
  assert.equal(p.isOpen, true);
  assert.equal(p.dirty, false);
  assert.equal(p.versionText, "Saves as version 1");
  assert.equal(p.badge.label, "Draft, not saved");
  assert.equal(gateway.saves.length, 0);
});

test("without a seed the page reads the state itself", async () => {
  const gateway = fakeGateway(state());
  const m = mount(designSystemPage({ gateway, clock: fixedClock("2026-10-01T10:30:00Z") }), { el: seededElement({ project_id: "p1", base: "/b", tab: "", state: null }) });
  m.instance.init();
  assert.equal(m.instance.loaded, false);
  await Promise.resolve();
  await Promise.resolve();
  assert.equal(m.instance.loaded, true);
  assert.equal(m.instance.rows.length, 1);
});

test("the tab comes from the address and a switch puts it back", () => {
  const { p, pushed } = page(state(), { tab: "history" });
  assert.equal(p.tab, "history");
  p.openTab("tokens");
  p.openTab("principles");
  assert.deepEqual(pushed, ["/projects/p1/design-system", "/projects/p1/design-system?tab=principles"]);
});

test("adding a token opens a row in the filtered group and the page is dirty", () => {
  const { p } = page();
  p.addToken();
  assert.equal(p.rows.length, 2);
  assert.equal(p.rows[1].group, "colors");
  assert.equal(p.rows[1].type, "color");
  assert.equal(p.dirty, true);
  assert.equal(p.changeSummary, "Unsaved: 1 added");
  assert.equal(p.statusWord(p.rows[1]), "added");
});

test("save sends the whole record, principles and components included, against the held version", async () => {
  const { p, gateway } = page();
  p.addToken();
  p.rows[1].name = "ink";
  p.rows[1].text = "#111";
  p.message = " Add ink ";
  await p.save();
  const sent = gateway.saves[0];
  assert.equal(sent.base, 3);
  assert.equal(sent.message, "Add ink");
  assert.equal(sent.record.principles, "# Calm\n");
  assert.deepEqual(sent.record.components, [{ slug: "button" }]);
  assert.deepEqual(sent.record.tokens.map((t) => t.name), ["signal", "ink"]);
  assert.equal(p.version, 4);
  assert.equal(p.dirty, false);
  assert.equal(p.versionText, "Saves as version 5");
  assert.equal(p.message, "");
});

test("an invalid save lands each error on its row, keeps the draft and stores nothing", async () => {
  const { p, gateway } = page();
  p.addToken();
  p.rows[1].name = "signal";
  gateway.refuse = invalid([
    { path: "/tokens/1", code: "DUPLICATE_TOKEN", message: "token colors.signal is defined more than once" },
    { path: "/tokens/1/value", code: "BAD_VALUE", message: "the value is empty" },
  ]);
  await p.save();
  assert.equal(p.version, 3);
  assert.equal(p.rows.length, 2);
  assert.deepEqual(p.cellErrors(p.rows[1], "name").map((e) => e.code), ["DUPLICATE_TOKEN"]);
  assert.deepEqual(p.cellErrors(p.rows[1], "value").map((e) => e.code), ["BAD_VALUE"]);
  assert.equal(p.cellErrors(p.rows[0], "name").length, 0);
  assert.equal(p.hasProblems, true);
  assert.equal(p.problemsText, "2 problems stop this from saving. Fix the marked fields.");
  p.touch(p.rows[1]);
  assert.equal(p.hasProblems, false);
});

test("a save over a newer version is refused with a notice naming it, and the edits stay", async () => {
  const { p, gateway } = page();
  p.rows[0].note = "mine";
  gateway.refuse = changed(5);
  await p.save();
  assert.equal(p.conflict, "save");
  assert.equal(p.conflictText, "Version 5 was saved elsewhere. Your edits are kept and were not saved.");
  assert.equal(p.rows[0].note, "mine");
  assert.equal(p.canSave, false);
  gateway.refuse = null;
  gateway.server = state({ version: 5 });
  await p.reload();
  assert.equal(p.conflict, "");
  assert.equal(p.version, 5);
  assert.equal(p.rows[0].note, "");
});

test("a save announced on the feed reloads quietly when nothing is edited", async () => {
  const { p, gateway } = page();
  gateway.server = state({ version: 4, record: { tokens: [colour, { ...colour, name: "ink" }], principles: "", components: [] } });
  p.followed({ version: 4 });
  await Promise.resolve();
  await Promise.resolve();
  assert.equal(p.version, 4);
  assert.equal(p.rows.length, 2);
  assert.equal(p.conflict, "");
});

test("a save announced over unsaved edits shows the notice and changes nothing else", () => {
  const { p } = page();
  p.rows[0].note = "mine";
  p.followed({ version: 4 });
  assert.equal(p.conflict, "feed");
  assert.equal(p.conflictText, "Version 4 was saved elsewhere. Your edits are kept and were not saved.");
  assert.equal(p.version, 3);
  assert.equal(p.rows[0].note, "mine");
});

test("the echo of our own save is not a conflict, whether it comes before or after the answer", async () => {
  const { p, gateway } = page();
  p.rows[0].note = "mine";
  const save = p.save();
  gateway.feed.onSaved({ version: 4, source: "manual" }); // before the answer
  await save;
  assert.equal(p.conflict, "");
  assert.equal(p.version, 4);
  gateway.feed.onSaved({ version: 4, source: "manual" }); // after it
  assert.equal(p.conflict, "");
  assert.equal(p.dirty, false);
});

test("a save from elsewhere that lands during ours is judged once ours answers", async () => {
  const { p, gateway } = page();
  p.rows[0].note = "mine";
  // Another save lands right after ours: the server is at 6 when ours is read back.
  gateway.afterSave = () => {
    gateway.feed.onSaved({ version: 4, source: "manual" });
    gateway.feed.onSaved({ version: 5, source: "manual" });
    gateway.server = state({ version: 5, record: { ...gateway.server.record, tokens: [colour, { ...colour, name: "theirs" }] } });
  };
  await p.save();
  await Promise.resolve();
  await Promise.resolve();
  assert.equal(p.version, 5, "a clean page follows it");
  assert.equal(p.rows.length, 2);
  assert.equal(p.conflict, "");
});

test("a reconnected feed asks the server what it missed", async () => {
  const { p, gateway } = page();
  p.rows[0].note = "mine";
  gateway.server = state({ version: 5 });
  gateway.feed.onStatus(FeedStatus.RESYNCED);
  await Promise.resolve();
  await Promise.resolve();
  assert.equal(p.conflict, "feed");
  assert.equal(p.elsewhere, 5);
});

test("Discard returns to the saved version, or reloads when the page is out of date", async () => {
  const { p, gateway } = page();
  p.rows[0].note = "mine";
  p.principles = "changed";
  p.discard();
  assert.equal(p.rows[0].note, "");
  assert.equal(p.principles, "# Calm\n");
  assert.equal(p.dirty, false);
  p.rows[0].note = "mine";
  p.followed({ version: 4 });
  gateway.server = state({ version: 4 });
  p.discard();
  await Promise.resolve();
  await Promise.resolve();
  assert.equal(p.version, 4);
  assert.equal(p.conflict, "");
});

test("rename is offered only on a saved token of a page with nothing to save, and saves as its own version", async () => {
  const { p, gateway } = page();
  assert.equal(p.canRename(p.rows[0]), true);
  p.rows[0].note = "x";
  assert.equal(p.canRename(p.rows[0]), false);
  assert.equal(p.renameHint(p.rows[0]), "Save or discard your edits first");
  p.rows[0].note = "";
  p.addToken();
  assert.equal(p.canRename(p.rows[1]), false);
  p.removeToken(p.rows[1]);

  p.startRename(p.rows[0]);
  assert.equal(p.canApplyRename, false, "the same name is not a rename");
  p.renameName = "brand";
  assert.equal(p.canApplyRename, true);
  await p.applyRename();
  assert.deepEqual(gateway.renames, [{ base: 3, from: { group: "colors", name: "signal" }, to: { group: "colors", name: "brand" } }]);
  assert.equal(p.version, 4);
  assert.equal(p.rows[0].name, "brand");
  assert.equal(p.renamingId, "");
});

test("a refused rename shows the server's code on the rename form", async () => {
  const { p, gateway } = page();
  p.startRename(p.rows[0]);
  p.renameName = "Bad Name";
  gateway.refuse = invalid([{ path: "/tokens/0/name", code: "BAD_NAME", message: "name must match" }]);
  await p.applyRename();
  assert.deepEqual(p.renameError, { code: "BAD_NAME", message: "name must match" });
  assert.equal(p.isRenaming(p.rows[0]), true);
  assert.equal(p.version, 3);
});

test("the filter and the group chips narrow the table; the counts say how many", () => {
  const { p } = page(state({ record: { tokens: [colour, { ...colour, name: "ink" }, { group: "radius", name: "md", type: "dimension", value: "8px", note: "" }], principles: "", components: [] } }));
  assert.equal(p.visibleRows.length, 3);
  assert.deepEqual(p.groups, [{ group: "colors", count: 2 }, { group: "radius", count: 1 }]);
  p.pickGroup("radius");
  assert.equal(p.visibleRows.length, 1);
  p.pickGroup("radius");
  assert.equal(p.groupFilter, "");
  p.filter = "zzz";
  assert.equal(p.noMatch, true);
});

const flush = async () => {
  for (let i = 0; i < 6; i++) await Promise.resolve();
};
const entry = (version, over = {}) => ({
  version,
  at: "2026-10-01T10:00:00Z",
  by: "person",
  source: "manual",
  proposalId: "",
  message: "",
  summary: { tokens: "+0 ~0 -0", principles: "+0 ~0 -0", components: "+0 ~0 -0" },
  ...over,
});

test("principles are written in the page's draft: editing them alone makes the page dirty and Save writes them with the tokens", async () => {
  const { p, gateway } = page();
  assert.equal(p.principlesMode, "write");
  p.principles = "# Calm\n\n## Colour\n\nOne signal.\n";
  assert.equal(p.dirty, true);
  assert.equal(p.changeSummary, "Unsaved: principles changed");
  assert.equal(p.canSave, true);
  await p.save();
  assert.equal(gateway.saves[0].record.principles, "# Calm\n\n## Colour\n\nOne signal.\n");
  assert.equal(gateway.saves[0].record.tokens.length, 1);
  assert.equal(p.dirty, false);
});

test("the outline follows the text, and Preview and Write switch the view", () => {
  const { p } = page();
  assert.equal(p.outline.length, 0);
  assert.equal(p.hasOutline, false);
  p.principles = "## One\n## Two\n";
  assert.deepEqual(p.outline.map((s) => s.title), ["One", "Two"]);
  assert.equal(p.hasOutline, true);
  p.showPreview();
  assert.equal(p.previewShown, true);
  assert.equal(p.writeShown, false);
  p.showWrite();
  assert.equal(p.writeShown, true);
});

test("an outline entry returns to Write and puts the cursor and the scroll at its section", () => {
  const { p, tick } = page();
  const calls = [];
  const area = { focus: () => calls.push("focus"), setSelectionRange: (a, b) => calls.push(["select", a, b]), scrollTop: 0, style: {} };
  p.principles = "intro\n\n## One\ntext\n## Two\n";
  p.showPreview();
  p.jumpTo(p.outline[1], area, 20);
  assert.equal(p.principlesMode, "write");
  tick();
  assert.deepEqual(calls, ["focus", ["select", p.principles.indexOf("## Two"), p.principles.indexOf("## Two")]]);
  assert.equal(area.scrollTop, 4 * 20);
});

test("the preview paints Markdown into its element and never raw HTML", () => {
  const { p } = page();
  const el = { innerHTML: "" };
  p.principles = "## Calm\n\n<script>x</script>\n";
  p.paintPrinciples(el);
  assert.equal(el.innerHTML, "");
  p.showPreview();
  p.paintPrinciples(el);
  assert.equal(el.innerHTML.includes("<h2>Calm</h2>"), true);
  assert.equal(el.innerHTML.includes("<script>"), false);
});

test("discarding returns the principles to the saved text", () => {
  const { p } = page();
  p.principles = "changed";
  p.discard();
  assert.equal(p.principles, "# Calm\n");
  assert.equal(p.dirty, false);
});

test("the History tab reads the versions when it opens, newest first, as rows in words", async () => {
  const { p, gateway } = page();
  gateway.list = [entry(3, { message: "Warmer", summary: { tokens: "+0 ~1 -0", principles: "+0 ~0 -0", components: "+0 ~0 -0" } }), entry(2), entry(1)];
  assert.equal(gateway.asked.length, 0);
  p.openTab("history");
  await flush();
  assert.deepEqual(p.versionRows.map((r) => r.version), [3, 2, 1]);
  assert.equal(p.versionRows[0].summary, "tokens ~1");
  assert.equal(p.versionRows[0].message, "Warmer");
  assert.equal(p.hasMoreVersions, false);
  assert.equal(p.historyLoaded, true);
});

test("a page that opens on History reads the versions at once", async () => {
  const { p, gateway } = page(state(), { tab: "history" });
  await flush();
  assert.equal(gateway.asked.length, 1);
  assert.equal(p.historyLoaded, true);
});

test("a long list loads older versions on demand, from the oldest one held", async () => {
  const { p, gateway } = page();
  gateway.list = Array.from({ length: 45 }, (_, i) => entry(45 - i));
  p.openTab("history");
  await flush();
  assert.equal(p.versionRows.length, 30);
  assert.equal(p.hasMoreVersions, true);
  await p.loadMoreVersions();
  assert.deepEqual(gateway.asked.at(-1), { limit: 30, before: 16 });
  assert.equal(p.versionRows.length, 45);
  assert.equal(p.hasMoreVersions, false);
});

test("a save announced on the feed refreshes the list, with no reload", async () => {
  const { p, gateway } = page();
  gateway.list = [entry(3), entry(2)];
  p.openTab("history");
  await flush();
  assert.equal(p.versionRows.length, 2);
  gateway.list = [entry(4, { message: "Elsewhere" }), ...gateway.list];
  gateway.feed.onSaved({ version: 4, source: "manual" });
  await flush();
  assert.deepEqual(p.versionRows.map((r) => r.version), [4, 3, 2]);
  assert.equal(p.versionRows[0].message, "Elsewhere");
});

test("a save made here adds its row to a list already read", async () => {
  const { p, gateway } = page();
  gateway.list = [entry(3), entry(2)];
  p.openTab("history");
  await flush();
  p.principles = "new";
  gateway.afterSave = () => (gateway.list = [entry(4, { message: "mine" }), ...gateway.list]);
  p.message = "mine";
  await p.save();
  await flush();
  assert.deepEqual(p.versionRows.map((r) => r.version), [4, 3, 2]);
});

test("a list that cannot be read says so and can be tried again", async () => {
  const { p, gateway } = page();
  const read = gateway.versions;
  gateway.versions = async () => {
    throw new StructuredError("INTERNAL", "down", 500);
  };
  p.openTab("history");
  await flush();
  assert.equal(p.hasHistoryError, true);
  gateway.versions = read;
  gateway.list = [entry(1)];
  await p.loadVersions();
  assert.equal(p.hasHistoryError, false);
  assert.equal(p.versionRows.length, 1);
});

test("a version opens read-only with its tokens as rows, its principles and the copyable values; closing returns to the list", async () => {
  const { p, gateway } = page();
  gateway.full[2] = { version: 2, at: "2026-09-30T10:00:00Z", record: { tokens: [colour, { group: "shadows", name: "card", type: "shadow", value: { blur: "8px", offsetY: "2px" }, note: "" }], principles: "## Calm\n", components: [] } };
  await p.openVersion(2);
  assert.equal(p.viewing, true);
  assert.equal(p.openedLabel, "Version 2");
  assert.deepEqual(p.openedRows.map((r) => `${r.group}.${r.name}=${r.text}`), ["colors.signal=#0a0", "shadows.card=blur: 8px; offsetY: 2px"]);
  assert.equal(p.openedHasPrinciples, true);
  p.closeVersion();
  assert.equal(p.viewing, false);
});

test("a version that is not there is told, not shown empty", async () => {
  const { p } = page();
  await p.openVersion(9);
  assert.equal(p.viewing, false);
  assert.equal(p.hasHistoryError, true);
});

test("copy puts a value or the principles on the clipboard and says it did", async () => {
  const copied = [];
  const { p, gateway } = page(state(), {}, { clipboard: { copyText: async (t) => (copied.push(t), true) } });
  gateway.full[1] = { version: 1, at: "2026-09-30T10:00:00Z", record: { tokens: [colour], principles: "## Calm\n", components: [] } };
  await p.openVersion(1);
  await p.copyValue(p.openedRows[0]);
  assert.deepEqual(copied, ["#0a0"]);
  assert.equal(p.copiedNote, "Copied colors.signal");
  await p.copyOpenedPrinciples();
  assert.equal(copied[1], "## Calm\n");
  assert.equal(p.copiedNote, "Copied the principles");
});
