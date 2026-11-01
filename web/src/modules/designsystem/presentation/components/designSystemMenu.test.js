import { StructuredError } from "../../../../shared/domain/errors.js";
import { mount, seededElement } from "../../../../shared/testing/alpine.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { designSystemMenu } from "./designSystemMenu.js";

file("designsystem/presentation/components/designSystemMenu");

const typed = (v) => ({ target: { value: v } });

/** The menu inside the page's scope: the page holds the design system's id, name, description and version. */
function menu(over = {}) {
  const g = {
    updates: [],
    removed: [],
    refuse: null,
    async update(id, parts) {
      g.updates.push({ id, ...parts });
      if (g.refuse) throw g.refuse;
    },
    async remove(id, confirmName) {
      g.removed.push({ id, confirmName });
      if (g.refuse) throw g.refuse;
    },
  };
  const el = seededElement({ index_href: "/projects/p1/design-systems" });
  const went = [];
  const m = mount(designSystemMenu({ gateway: g, navigate: (url) => went.push(url) }), { el });
  Object.assign(m.instance, { designSystemId: "ds1", name: "App", description: "Product apps", version: 3, ...over });
  m.instance.init();
  return { g, went, m: m.instance };
}

test("Rename opens a panel holding the current name, with the slug read only", () => {
  const { m } = menu();
  m.menuSelect({ detail: { action: "rename" } });
  assert.deepEqual([m.panel, m.draftName, m.renaming], ["rename", "App", true]);
});

test("a rename sends only the name and closes the panel", async () => {
  const { m, g } = menu();
  m.menuSelect({ detail: { action: "rename" } });
  assert.equal(m.canSave, false);
  m.typeName(typed("Apps"));
  assert.equal(m.canSave, true);
  await m.save();
  assert.deepEqual(g.updates, [{ id: "ds1", name: "Apps" }]);
  assert.equal(m.panel, "");
});

test("a name another design system has is refused under the field and the panel stays open", async () => {
  const { m, g } = menu();
  m.menuSelect({ detail: { action: "rename" } });
  m.typeName(typed("Brand"));
  g.refuse = new StructuredError("SLUG_TAKEN", "the project already has a design system with that name", 409, { field: "name" });
  await m.save();
  assert.deepEqual([m.panel, m.fault?.code], ["rename", "SLUG_TAKEN"]);
});

test("Edit description sends only the description, and may empty it", async () => {
  const { m, g } = menu();
  m.menuSelect({ detail: { action: "description" } });
  assert.deepEqual([m.panel, m.draftDescription], ["description", "Product apps"]);
  m.typeDescription(typed(""));
  await m.save();
  assert.deepEqual(g.updates, [{ id: "ds1", description: "" }]);
});

test("Delete asks for the name, and a wrong name does nothing", async () => {
  const { m, g, went } = menu();
  m.menuSelect({ detail: { action: "delete" } });
  assert.deepEqual([m.panel, m.canDelete], ["delete", false]);
  m.typeConfirm(typed("Ap"));
  assert.equal(m.canDelete, false);
  await m.remove();
  assert.deepEqual([g.removed, went], [[], []]);
});

test("the right name deletes the design system and goes back to the list", async () => {
  const { m, g, went } = menu();
  m.menuSelect({ detail: { action: "delete" } });
  m.typeConfirm(typed("App"));
  assert.equal(m.canDelete, true);
  await m.remove();
  assert.deepEqual(g.removed, [{ id: "ds1", confirmName: "App" }]);
  assert.deepEqual(went, ["/projects/p1/design-systems"]);
});

test("a design system in use is refused naming its repositories, and stays", async () => {
  const { m, g, went } = menu();
  m.menuSelect({ detail: { action: "delete" } });
  m.typeConfirm(typed("App"));
  g.refuse = new StructuredError("DESIGN_SYSTEM_IN_USE", "in use", 409, { repositories: [{ repository_id: "r1", name: "spillway" }, { repository_id: "r2", name: "spillway-web" }] });
  await m.remove();
  assert.deepEqual([m.refused, m.refusedTitle, m.refusedRepositories], [true, "“App” cannot be deleted while repositories use it.", "spillway, spillway-web"]);
  assert.deepEqual(went, []);
});

test("closing the panel forgets what was typed and the refusal", () => {
  const { m } = menu();
  m.menuSelect({ detail: { action: "delete" } });
  m.typeConfirm(typed("App"));
  m.close();
  assert.deepEqual([m.panel, m.confirm, m.refused], ["", "", false]);
});

test("the delete panel says how much goes with it", () => {
  assert.equal(menu().m.deleteNote, "This removes its 3 saved versions for good.");
  {
    const { m } = menu({ version: 0 });
    assert.equal(m.deleteNote, "It has no saved versions yet.");
  }
});
