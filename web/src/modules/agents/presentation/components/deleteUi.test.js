import { StructuredError } from "../../../../shared/domain/errors.js";
import { mount, seededElement } from "../../../../shared/testing/alpine.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { memoryCatalog } from "../../infrastructure/memory-gateway.js";
import { makeWorld } from "../../testing/fixtures.js";
import { agentsPage } from "./agentsPage.js";

file("agents/presentation/components/deleteUi");

async function open(path, wrap = (g) => g) {
  const world = makeWorld();
  world.catalog.bundles.push({ name: "extra", entries: ["tdd"], resolved_skills: ["tdd"], used_by_agents: ["backend"], used_by_bundles: [] });
  const { gateway, server } = memoryCatalog(world);
  const pushed = [];
  const location = { pathname: path, search: "" };
  const history = { pushState: (_s, _t, url) => { pushed.push(url); location.pathname = url.split("?")[0]; } };
  const seed = await server.api.get("/get_agent_catalog");
  const m = mount(agentsPage({ gateway: wrap(gateway), history, location }), { el: seededElement(seed) });
  await m.instance.init();
  return { ...m, server, pushed, page: m.instance };
}

/** The actions menu's event for Delete: the wrapper holds a trigger the focus comes back to. */
function menuEvent() {
  const trigger = { focused: 0, isConnected: true, focus() { this.focused++; } };
  return { trigger, event: { detail: { action: "delete" }, currentTarget: { querySelector: () => trigger } } };
}
const key = (k, extra = {}) => ({ key: k, shiftKey: false, prevented: false, preventDefault() { this.prevented = true; }, ...extra });
const requests = (server, verb) => server.requests.filter(([m, p]) => m === "POST" && p === `/${verb}`);

test("Delete in a bundle's menu opens the dialog with who it reaches, and Delete stays off until the name is typed exactly", async () => {
  const { page } = await open("/agents/bundles/ux");
  const { event } = menuEvent();
  assert.equal(page.del, null);
  await page.menuSelect(event);
  assert.equal(page.del.title, "Delete bundle “@ux”?");
  assert.deepEqual(page.del.groups.find((g) => g.key === "agents").items.map((i) => i.name), ["ux-designer"]);
  assert.equal(page.delReady, false);
  for (const wrong of ["", "@ux", "UX", "ux ", "u"]) {
    page.delTyped = wrong;
    assert.equal(page.delReady, false, `"${wrong}" is not the name`);
  }
  page.delTyped = "ux";
  assert.equal(page.delReady, true);
  assert.ok(page.del.note.includes("not in git"));
});

test("Delete does nothing while the name is not typed", async () => {
  const { page, server } = await open("/agents/bundles/ux");
  await page.menuSelect(menuEvent().event);
  await page.confirmDelete();
  assert.equal(requests(server, "delete_catalog_bundle").length, 0);
  assert.ok(page.del !== null, "the dialog stays");
});

test("deleting a bundle sends the versions it read, shows the toast with Undo, refreshes the library and goes back to the tab; Undo puts it back", async () => {
  const { page, server, pushed } = await open("/agents/bundles/ux");
  await page.menuSelect(menuEvent().event);
  page.delTyped = "ux";
  await page.confirmDelete();
  const [, , body] = requests(server, "delete_catalog_bundle")[0];
  assert.deepEqual(body, { name: "ux", base_versions: { "agents.json": "sha256:a1" } });
  assert.equal(page.del, null, "the dialog closed");
  assert.deepEqual(pushed, ["/agents/bundles"]);
  assert.equal(page.tab, "bundles");
  assert.equal(page.item, "");
  assert.deepEqual(page.bundleRowsView.map((r) => r.key), ["extra"]);
  assert.ok(page.toast.text.includes("Deleted bundle @ux"), page.toast.text);
  assert.equal(page.toast.hasUndo, true);

  await page.undoLast();
  assert.deepEqual(page.bundleRowsView.map((r) => r.key).sort(), ["extra", "ux"]);
  assert.equal(page.toast.hasUndo, false);
});

test("each kind opens its own dialog from its tab and returns to that tab", async () => {
  for (const [path, tab, title, typed] of [
    ["/agents/backend", "agents", "Delete agent “backend”?", "backend"],
    ["/agents/skills/lint", "skills", "Delete skill “lint”?", "lint"],
    ["/agents/mcp/github", "mcp", "Delete MCP server “github”?", "github"],
  ]) {
    const { page, pushed } = await open(path);
    await page.menuSelect(menuEvent().event);
    assert.equal(page.del.title, title);
    page.delTyped = typed;
    await page.confirmDelete();
    assert.equal(page.del, null, path);
    assert.equal(page.tab, tab);
    assert.deepEqual(pushed, [`/agents${tab === "agents" ? "" : `/${tab}`}`]);
    assert.ok(page.toast.hasUndo);
  }
});

test("Escape and Cancel close the dialog and give the focus back to the menu button", async () => {
  const { page } = await open("/agents/skills/lint");
  const { event, trigger } = menuEvent();
  await page.menuSelect(event);
  page.delTyped = "li";
  const esc = key("Escape");
  page.deleteKey(esc);
  assert.equal(esc.prevented, true);
  assert.equal(page.del, null);
  assert.equal(page.delTyped, "", "what was typed is forgotten");
  assert.equal(trigger.focused, 1);

  await page.menuSelect(event);
  page.cancelDelete();
  assert.equal(page.del, null);
  assert.equal(trigger.focused, 2);
});

test("Tab stays inside the dialog: from the last control it wraps to the first, and Shift+Tab the other way", async () => {
  const { page } = await open("/agents/skills/lint");
  await page.menuSelect(menuEvent().event);
  const mk = (name) => ({ name, focused: 0, focus() { this.focused++; } });
  const [first, middle, last] = [mk("first"), mk("middle"), mk("last")];
  const dialog = { querySelectorAll: () => [first, middle, last] };
  const doc = { activeElement: last };
  const tab = key("Tab", { currentTarget: dialog, target: last });
  page.deleteKey(tab, doc);
  assert.equal(tab.prevented, true);
  assert.equal(first.focused, 1);

  doc.activeElement = first;
  const back = key("Tab", { shiftKey: true, currentTarget: dialog, target: first });
  page.deleteKey(back, doc);
  assert.equal(back.prevented, true);
  assert.equal(last.focused, 1);

  doc.activeElement = middle;
  const free = key("Tab", { currentTarget: dialog, target: middle });
  page.deleteKey(free, doc);
  assert.equal(free.prevented, false, "inside the dialog the browser moves on its own");
});

test("an outside edit since the page read refuses the delete with the conflict; Overwrite deletes anyway", async () => {
  const { page, server } = await open("/agents/bundles/ux");
  await page.menuSelect(menuEvent().event);
  server.editOnDisk("agents.json");
  page.delTyped = "ux";
  await page.confirmDelete();
  assert.ok(page.del !== null, "the dialog stays");
  assert.equal(page.delView.conflict, true);
  assert.ok(page.delConflictTitle.includes("agents.json"));
  assert.equal(page.delReady, false, "Delete is off while the conflict stands");
  assert.equal(page.toast, null);

  await page.delOverwrite();
  assert.equal(page.del, null);
  assert.deepEqual(page.bundleRowsView.map((r) => r.key), ["extra"]);
  assert.ok(page.toast.hasUndo);
});

test("Reload on the conflict reads the catalog again and keeps the dialog, with what the item reaches now", async () => {
  const { page, server } = await open("/agents/bundles/ux");
  await page.menuSelect(menuEvent().event);
  server.editOnDisk("agents.json");
  page.delTyped = "ux";
  await page.confirmDelete();
  await page.delReload();
  assert.ok(page.del !== null, "the dialog stays");
  assert.equal(page.delView.conflict, false);
  assert.equal(page.delReady, true, "what was typed is kept");
  await page.confirmDelete();
  assert.equal(page.del, null);
  assert.deepEqual(page.bundleRowsView.map((r) => r.key), ["extra"]);
});

test("a refusal other than a conflict is shown in the dialog with its code and what to do", async () => {
  const gone = (g) => ({ ...g, deleteBundle: async () => { throw new StructuredError("BUNDLE_NOT_FOUND", "no bundle ux"); } });
  const { page } = await open("/agents/bundles/ux", gone);
  await page.menuSelect(menuEvent().event);
  page.delTyped = "ux";
  await page.confirmDelete();
  assert.ok(page.del !== null, "the dialog stays");
  assert.equal(page.delView.conflict, false);
  assert.equal(page.delErrorCode, "BUNDLE_NOT_FOUND");
  assert.equal(page.delErrorText, "That bundle no longer exists.");
  assert.equal(page.delBusy, false);
  assert.equal(page.toast, null);
});

test("Copy path still works beside Delete, and the menu's other actions do not open the dialog", async () => {
  const { page } = await open("/agents/bundles/ux");
  await page.menuSelect({ detail: { action: "copy-path" }, currentTarget: {} });
  assert.equal(page.del, null);
});
