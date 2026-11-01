import { StructuredError } from "../../../../shared/domain/errors.js";
import { FeedStatus } from "../../../../shared/domain/feed.js";
import { listRow } from "../../domain/identity.js";
import { mount, seededElement } from "../../../../shared/testing/alpine.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { designSystemsPage } from "./designSystemsPage.js";

file("designsystem/presentation/pages/designSystemsPage");

const dto = (name, slug, over = {}) => ({ design_system_id: `ds-${slug}`, name, slug, description: "", version: 0, repositories: [], open_proposals: 0, ...over });
const typed = (v) => ({ target: { value: v } });

function page(rows = [dto("App", "app")]) {
  const g = {
    rows: rows.map(listRow),
    created: [],
    refuse: null,
    followed: null,
    async list() {
      return g.rows.slice();
    },
    async create(projectId, input) {
      g.created.push({ projectId, ...input });
      if (g.refuse) throw g.refuse;
      g.rows.push(listRow(dto(input.name, input.slug, { description: input.description })));
      return "ds-new";
    },
    followProject(projectId, handlers) {
      g.followed = [projectId, handlers];
      return () => (g.followed = null);
    },
  };
  const el = seededElement({ project_id: "p1", index_href: "/projects/p1/design-systems", design_systems: rows });
  const went = [];
  const m = mount(designSystemsPage({ gateway: g, navigate: (url) => went.push(url) }), { el });
  m.instance.init();
  return { g, went, ...m, p: m.instance };
}

test("the list opens on the rows the server seeded, ordered by name", () => {
  const { p } = page([dto("Marketing", "marketing", { version: 1 }), dto("App", "app")]);
  assert.deepEqual(p.rowsView.map((r) => [r.name, r.versionLabel, r.href]), [["App", "v0", "/projects/p1/design-systems/app"], ["Marketing", "v1", "/projects/p1/design-systems/marketing"]]);
  assert.deepEqual([p.isEmpty, p.hasRows], [false, true]);
});

test("a project with no design system shows the three starts, and no New design system button", () => {
  const { p } = page([]);
  assert.deepEqual([p.isEmpty, p.showStarts, p.showNew], [true, true, false]);
});

test("the seeded rows are completed with the repositories that use each", async () => {
  const { p, g } = page([dto("App", "app")]);
  g.rows = [listRow(dto("App", "app", { repositories: [{ repository_id: "r1", name: "spillway" }] }))];
  await p.load();
  assert.equal(p.rowsView[0].usedBy, "spillway");
});

test("New design system opens the identity form and Cancel closes it with nothing stored", () => {
  const { p, g } = page();
  p.openNew();
  assert.deepEqual([p.formOpen, p.identityStep], [true, true]);
  p.cancel();
  assert.equal(p.formOpen, false);
  assert.deepEqual(g.created, []);
});

test("the slug follows the name until the person edits the slug", () => {
  const { p } = page();
  p.openNew();
  p.typeName(typed("Marketing site"));
  assert.equal(p.slug, "marketing-site");
  p.typeSlug(typed("site"));
  p.typeName(typed("Marketing site 2"));
  assert.equal(p.slug, "site");
});

test("Continue needs a name and a slug, then offers the three starts", () => {
  const { p } = page();
  p.openNew();
  assert.equal(p.canContinue, false);
  p.typeName(typed("Marketing"));
  assert.equal(p.canContinue, true);
  p.continueForm();
  assert.deepEqual([p.identityStep, p.startStep], [false, true]);
  p.back();
  assert.equal(p.identityStep, true);
});

test("Start blank creates the design system and opens its page by slug", async () => {
  const { p, g, went } = page();
  p.openNew();
  p.typeName(typed("Marketing"));
  p.description = "Campaign pages";
  p.continueForm();
  await p.startBlank();
  assert.deepEqual(g.created, [{ projectId: "p1", name: "Marketing", slug: "marketing", description: "Campaign pages" }]);
  assert.deepEqual(went, ["/projects/p1/design-systems/marketing"]);
});

test("from the empty project, Start blank asks for the identity first and then creates", async () => {
  const { p, g, went } = page([]);
  p.startFirst();
  assert.deepEqual([p.formOpen, p.submitLabel], [true, "Start blank"]);
  p.typeName(typed("App"));
  await p.submit();
  assert.deepEqual(g.created.map((c) => c.slug), ["app"]);
  assert.deepEqual(went, ["/projects/p1/design-systems/app"]);
});

test("a taken slug shows SLUG_TAKEN under its field, creates nothing and returns to the identity", async () => {
  const { p, g, went } = page();
  p.openNew();
  p.typeName(typed("App"));
  p.continueForm();
  g.refuse = new StructuredError("SLUG_TAKEN", "the project already has a design system with that slug", 409, { field: "slug" });
  await p.startBlank();
  assert.deepEqual([p.identityStep, p.slugFault?.code, p.nameFault], [true, "SLUG_TAKEN", null]);
  assert.deepEqual(went, []);
  p.typeSlug(typed("app-2"));
  assert.equal(p.slugFault, null);
});

test("a taken name shows under the name field", async () => {
  const { p, g } = page();
  p.startFirst();
  p.typeName(typed("App"));
  g.refuse = new StructuredError("SLUG_TAKEN", "the project already has a design system with that name", 409, { field: "name" });
  await p.submit();
  assert.deepEqual([p.nameFault?.code, p.slugFault], ["SLUG_TAKEN", null]);
});

test("a refusal that names no field shows as an error of the form", async () => {
  const { p, g } = page();
  p.startFirst();
  p.typeName(typed("App"));
  g.refuse = new StructuredError("NETWORK", "The harness server is not reachable.");
  await p.submit();
  assert.deepEqual([p.formError?.code, p.nameFault, p.slugFault], ["NETWORK", null, null]);
});

test("the list follows the project feed and reads again", async () => {
  const { p, g } = page();
  g.rows.push(listRow(dto("Brand", "brand")));
  g.followed[1].onChanged();
  await Promise.resolve();
  await Promise.resolve();
  assert.deepEqual(p.rowsView.map((r) => r.name), ["App", "Brand"]);
  g.followed[1].onStatus(FeedStatus.RESYNCED);
});

test("a design system created meanwhile is not lost while the form is open", async () => {
  const { p, g } = page();
  p.openNew();
  p.typeName(typed("Mine"));
  g.rows.push(listRow(dto("Brand", "brand")));
  await p.load();
  assert.deepEqual([p.name, p.formOpen], ["Mine", true]);
});
