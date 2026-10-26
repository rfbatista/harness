import { mount, seededElement } from "../../../../shared/testing/alpine.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { memoryCatalog } from "../../infrastructure/memory-gateway.js";
import { makeWorld } from "../../testing/fixtures.js";
import { agentsPage } from "./agentsPage.js";

file("agents/presentation/components/bundleEditor");

function fakeTimers() {
  const pending = new Map();
  let id = 0;
  return {
    setTimeout: (fn) => { pending.set(++id, fn); return id; },
    clearTimeout: (n) => { pending.delete(n); },
    async fire() {
      const fns = [...pending.values()];
      pending.clear();
      for (const fn of fns) await fn();
    },
  };
}

/** The page at `path`, over the memory catalog, with a second bundle so there is something to nest. */
async function open(path, { search = "" } = {}) {
  const world = makeWorld();
  world.catalog.bundles.push({ name: "extra", entries: ["tdd"], resolved_skills: ["tdd"], used_by_agents: [], used_by_bundles: [] });
  const { gateway, server } = memoryCatalog(world);
  const timers = fakeTimers();
  const pushed = [];
  const location = { pathname: path, search };
  const history = { pushState: (_s, _t, url) => { pushed.push(url); location.pathname = url.split("?")[0]; } };
  const seed = await server.api.get("/get_agent_catalog");
  const m = mount(agentsPage({ gateway, history, location, timers }), { el: seededElement(seed) });
  await m.instance.init();
  return { ...m, server, timers, pushed, location, page: m.instance };
}

const link = (href, key = "") => ({ button: 0, preventDefault() {}, currentTarget: { dataset: { key }, getAttribute: () => href } });
const typeName = (page, name) => { page.draft.name = name; page.draftChanged(); };
const saveRequests = (server) => server.requests.filter(([m, p]) => m === "POST" && p === "/save_catalog_bundle");

test("the Bundles tab lists bundles, narrows them, and shows a bundle's contents, size and who names it", async () => {
  const { page } = await open("/agents/bundles");
  assert.deepEqual(page.bundleRowsView.map((r) => r.name), ["@ux", "@extra"]);
  assert.equal(page.bundlesFoot, "agents.json › bundles · 2 bundles");
  assert.equal(page.tabState.bundles.count, "2");
  page.bundleFilter = "unused";
  assert.deepEqual(page.bundleRowsView.map((r) => r.name), ["@extra"]);
  page.bundleFilter = "all";
  page.bundleQuery = "lint";
  assert.deepEqual(page.bundleRowsView.map((r) => r.name), ["@ux"]);

  page.openBundleLink(link("/agents/bundles/ux", "ux"));
  assert.equal(page.bundle.title, "@ux");
  assert.equal(page.bundle.sizeLabel, "1 skill after nested bundles");
  assert.deepEqual(page.bundle.agents.map((a) => a.name), ["ux-designer"]);
  assert.equal(page.bundleRowsView[0].current, true);
  page.openBundleLink(link("/agents/bundles/extra", "extra"));
  assert.equal(page.bundle.unused, true);
  assert.equal(page.detailOpenAttr, "");
});

test("renaming a bundle names who will be rewritten, previews the diff, saves, and Undo puts it back", async () => {
  const { page, timers, server, pushed } = await open("/agents/bundles/ux/edit");
  assert.equal(page.editing, true);
  assert.equal(page.draft.name, "ux");
  assert.equal(page.sv.canSave, false, "nothing changed yet");
  assert.equal(page.renameBanner.show, false);

  typeName(page, "design");
  assert.equal(page.renameBanner.show, true);
  assert.equal(page.renameBanner.from, "@ux");
  assert.equal(page.renameBanner.to, "@design");
  assert.equal(page.renameBanner.text, "1 agent and 0 bundles (ux-designer)");
  assert.equal(page.sv.canSave, true);
  assert.equal(page.sv.footer, "1 unsaved change · writes agents.json");
  assert.equal(page.leaveGuarded, true);

  await timers.fire();
  assert.equal(page.sv.review, true);
  assert.equal(page.reviewFiles[0].path, "agents.json");
  assert.ok(page.reviewFiles[0].lines.some((l) => l.mark === "+" && l.text.includes("design")));
  assert.ok(page.reviewFiles[0].lines.some((l) => l.mark === "−" && l.text.includes('"ux"')));

  await page.saveEditor();
  assert.equal(page.draft, null, "the editor closes");
  assert.deepEqual(pushed, ["/agents/bundles/design"]);
  assert.equal(page.toast.text, "Saved agents.json — bundle @design");
  assert.equal(page.toast.hasUndo, true);
  assert.equal(page.toast.note, "Changes reach sessions from their next start. Running sessions keep what they started with.");
  assert.deepEqual(page.catalog.bundles.map((b) => b.name), ["design", "extra"]);
  assert.deepEqual(page.catalog.agents[0].entries, ["@design", "tdd"], "the reference moved with it");
  assert.equal(page.bundle.title, "@design");

  await page.undoLast();
  assert.deepEqual(page.catalog.bundles.map((b) => b.name), ["ux", "extra"]);
  assert.equal(page.toast.text, "Undid the last change");
  assert.equal(page.toast.hasUndo, false);
  assert.equal(server.requests.filter(([, p]) => p === "/undo_catalog_change").length, 1);
});

test("an Undo the server refuses says why", async () => {
  const { page, server } = await open("/agents/bundles/ux/edit");
  typeName(page, "design");
  await page.saveEditor();
  server.editOnDisk("agents.json");
  await page.undoLast();
  assert.equal(page.toast.tone, "danger");
  assert.equal(page.toast.text, "A file changed since this save, so Undo would lose that change. Nothing was undone.");
  assert.equal(page.toast.hasUndo, false);
});

test("an outside edit blocks the save and shows what changed; Overwrite writes anyway", async () => {
  const { page, timers, server } = await open("/agents/bundles/ux/edit");
  typeName(page, "design");
  await timers.fire();
  server.editOnDisk("agents.json");
  await page.saveEditor();
  assert.equal(page.sv.conflict, true);
  assert.equal(page.sv.canSave, false);
  assert.equal(page.draft.name, "design", "the edits are kept");
  assert.equal(page.conflictFiles[0].path, "agents.json");
  const real = () => saveRequests(server).filter(([, , b]) => !b.dry_run);
  assert.equal(real().length, 1);
  assert.equal(Boolean(real()[0][2].force), false);
  await page.overwrite();
  assert.equal(page.toast.text, "Saved agents.json — bundle @design");
  assert.equal(real().length, 2);
  assert.equal(real()[1][2].force, true);
  assert.equal(page.draft, null);
});

test("Reload drops my edits and reads the catalog again", async () => {
  const { page, server } = await open("/agents/bundles/ux/edit");
  typeName(page, "design");
  server.editOnDisk("agents.json");
  await page.saveEditor();
  assert.equal(page.sv.conflict, true);
  await page.reloadEditor();
  assert.equal(page.draft.name, "ux");
  assert.equal(page.sv.conflict, false);
  assert.equal(page.sv.dirty, false);
  assert.equal(page.leaveGuarded, false);
});

test("a bundle that would include itself, through another, is refused and shown on the entries field", async () => {
  const { page, timers, server } = await open("/agents/bundles/ux/edit");
  page.bundlePickerAdd("@extra");
  assert.deepEqual(page.draft.entries, ["lint", "@extra"]);
  await page.saveEditor();
  assert.equal(page.toast.text, "Saved agents.json — bundle @ux");

  page.openBundleLink(link("/agents/bundles/extra/edit", "extra"));
  assert.equal(page.editing, true);
  page.bundlePickerAdd("@ux");
  await timers.fire();
  assert.equal(page.fieldErrors.entries, "Bundles cannot include each other, directly or through another bundle.");
  assert.equal(page.entriesDescribedBy, "bundle-entries-hint bundle-entries-error");
  assert.equal(page.sv.canSave, false);
  assert.equal(server.requests.filter(([, p, b]) => p === "/save_catalog_bundle" && !b.dry_run).length, 1, "nothing was sent but the first save");

  page.bundlePickerUnpick({ currentTarget: { dataset: { key: "@ux" } } });
  assert.equal(page.fieldErrors.entries, "", "editing clears it");
});

test("Save is off while the name is invalid or there are no entries, and the name error is linked to its field", async () => {
  const { page } = await open("/agents/bundles/new");
  assert.equal(page.editing, true);
  assert.equal(page.editorTitle, "New bundle");
  assert.equal(page.sv.canSave, false);
  typeName(page, "@Mobile Dev");
  assert.equal(page.draft.name, "Mobile Dev", "the @ is the field's prefix");
  assert.equal(page.fieldErrors.name, "Bundle name uses lowercase letters, digits and hyphens, and starts with a letter or digit.");
  assert.equal(page.nameDescribedBy, "bundle-name-hint bundle-name-error");
  assert.equal(page.nameInvalid, true);
  typeName(page, "mobile-dev");
  assert.equal(page.fieldErrors.name, "");
  assert.equal(page.nameDescribedBy, "bundle-name-hint");
  assert.equal(page.sv.canSave, false, "no entries yet");
  page.bundlePickerAdd("tdd");
  assert.equal(page.sv.canSave, true);
  assert.deepEqual(page.bundlePickerView.chips.map((c) => c.label), ["tdd"]);
});

test("a name another bundle has is refused on the save, shown on the name with the suggestion", async () => {
  const { page } = await open("/agents/bundles/new");
  typeName(page, "extra");
  page.bundlePickerAdd("tdd");
  await page.saveEditor();
  assert.equal(page.fieldErrors.name, "That name is taken. Try extra-2.");
  assert.equal(page.draft.name, "extra", "the form is kept");
  assert.equal(page.sv.canSave, false);
  typeName(page, "extra-2");
  assert.equal(page.fieldErrors.name, "");
  await page.saveEditor();
  assert.equal(page.toast.text, "Saved agents.json — bundle @extra-2");
});

test("the picker lists bundles first, filters as you type, and adds with the keyboard", async () => {
  const { page } = await open("/agents/bundles/new");
  const press = (key) => { const ev = { key, defaultPrevented: false, preventDefault() { this.defaultPrevented = true; } }; page.bundlePickerKey(ev); return ev; };
  page.bundlePickerFocus();
  assert.equal(page.bundlePickerView.expanded, true);
  assert.deepEqual(page.bundlePickerView.options.map((o) => o.key), ["@extra", "@ux", "lint", "tdd"]);
  assert.equal(press("ArrowDown").defaultPrevented, true);
  assert.equal(page.bundlePickerView.activeId, "bundlePicker-opt-1");
  press("Enter");
  assert.deepEqual(page.draft.entries, ["@ux"]);
  assert.equal(page.bundlePickerView.options.find((o) => o.key === "@ux").chosen, true);
  page.bundlePickerQuery = "li";
  assert.deepEqual(page.bundlePickerView.options.map((o) => o.key), ["lint"]);
  page.bundlePickerQuery = "";
  press("Backspace");
  assert.deepEqual(page.draft.entries, [], "Backspace in an empty input removes the last chip");
  page.bundlePickerQuery = "nothing";
  assert.equal(page.bundlePickerView.empty, true);
  press("Escape");
  assert.equal(page.bundlePickerView.expanded, false);
});

test("leaving a dirty editor asks first: Keep editing stays, Discard goes", async () => {
  const tabEvent = (tab) => ({ button: 0, preventDefault() {}, currentTarget: { dataset: { tab } } });
  const clean = await open("/agents/bundles/ux/edit");
  clean.page.select(tabEvent("skills"));
  assert.equal(clean.page.tab, "skills", "a clean editor lets go without asking");
  assert.equal(clean.page.editing, false);

  const { page } = await open("/agents/bundles/ux/edit");
  typeName(page, "design");
  page.select(tabEvent("skills"));
  assert.equal(page.tab, "bundles", "still here");
  assert.equal(page.leaving !== null, true);
  assert.equal(page.draft.name, "design");
  page.keepEditing();
  assert.equal(page.leaving, null);
  assert.equal(page.draft.name, "design");
  page.select(tabEvent("skills"));
  page.discardAndLeave();
  assert.equal(page.tab, "skills");
  assert.equal(page.draft, null);
  assert.equal(page.leaving, null);
});

test("closing the window or following a link away from a dirty editor triggers the browser's own prompt", async () => {
  const { page } = await open("/agents/bundles/ux/edit");
  const ev = () => ({ defaultPrevented: false, returnValue: undefined, preventDefault() { this.defaultPrevented = true; } });
  const clean = ev();
  page.beforeUnload(clean);
  assert.equal(clean.defaultPrevented, false);
  typeName(page, "design");
  const dirty = ev();
  page.beforeUnload(dirty);
  assert.equal(dirty.defaultPrevented, true);
  assert.equal(dirty.returnValue, "");
});

test("going back from a dirty editor keeps the editor and asks", async () => {
  const { page, location, pushed } = await open("/agents/bundles/ux/edit");
  typeName(page, "design");
  location.pathname = "/agents/bundles";
  page.popped();
  assert.equal(page.editing, true);
  assert.equal(page.leaving !== null, true);
  assert.deepEqual(pushed, ["/agents/bundles/ux/edit"], "the address is put back");
  page.discardAndLeave();
  assert.equal(page.editing, false);
  assert.equal(page.tab, "bundles");
});

test("Cancel on a clean editor returns to the bundle; on a new one, to the list", async () => {
  const a = await open("/agents/bundles/ux/edit");
  a.page.cancelEditor({ preventDefault() {} });
  assert.deepEqual(a.pushed, ["/agents/bundles/ux"]);
  assert.equal(a.page.editing, false);
  const b = await open("/agents/bundles/new");
  b.page.cancelEditor({ preventDefault() {} });
  assert.deepEqual(b.pushed, ["/agents/bundles"]);
});

test("a bundle the catalog lacks has no editor", async () => {
  const { page } = await open("/agents/bundles/ghost/edit");
  assert.equal(page.editing, false);
  assert.equal(page.unknownBundle, true);
});
