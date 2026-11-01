import { StructuredError } from "../../../shared/domain/errors.js";
import { assert, file, test } from "../../../shared/testing/test.js";
import { faultOf, listRow, sortRows, suggestSlug } from "./identity.js";

file("designsystem/domain/identity");

test("the slug is suggested from the name: lowercase words joined by dashes", () => {
  assert.equal(suggestSlug("Marketing"), "marketing");
  assert.equal(suggestSlug("Product apps & admin"), "product-apps-admin");
  assert.equal(suggestSlug("  Spillway   Docs  "), "spillway-docs");
});

test("a suggested slug drops accents, and starts with a letter as every slug must", () => {
  assert.equal(suggestSlug("Café Années"), "cafe-annees");
  assert.equal(suggestSlug("2024 Brand"), "brand");
  assert.equal(suggestSlug("---"), "");
  assert.equal(suggestSlug(""), "");
});

test("a suggested slug is not longer than a name can be", () => {
  assert.ok(suggestSlug("a".repeat(100)).length <= 60);
  assert.equal(suggestSlug("ab ".repeat(30)).endsWith("-"), false);
});

test("a list row shows version 0 for an identity with no saved version, and names the repositories that use it", () => {
  const row = listRow({
    design_system_id: "ds1", name: "App", slug: "app", description: "Product apps", version: 0,
    repositories: [{ repository_id: "r1", name: "spillway", export_state: "never_exported" }, { repository_id: "r2", name: "spillway-web" }],
    open_proposals: 0,
  });
  assert.equal(row.versionLabel, "v0");
  assert.deepEqual(row.repositories.map((r) => r.name), ["spillway", "spillway-web"]);
  assert.equal(row.usedBy, "spillway, spillway-web");
  assert.equal(row.hasProposals, false);
  assert.equal(listRow({ design_system_id: "ds2", name: "Brand", slug: "brand", version: 4, repositories: [], open_proposals: 2 }).versionLabel, "v4");
});

test("a row without repositories or description reads as unused and undescribed", () => {
  const row = listRow({ design_system_id: "ds1", name: "Brand", slug: "brand" });
  assert.deepEqual([row.description, row.hasDescription, row.usedBy, row.unused, row.versionLabel], ["", false, "", true, "v0"]);
});

test("open proposals show only when there are some", () => {
  const row = listRow({ design_system_id: "ds1", name: "App", slug: "app", open_proposals: 2 });
  assert.deepEqual([row.hasProposals, row.proposalsLabel], [true, "2 open"]);
});

test("rows are ordered by name, ignoring case", () => {
  const rows = [{ name: "marketing" }, { name: "App" }, { name: "Brand" }];
  assert.deepEqual(sortRows(rows).map((r) => r.name), ["App", "Brand", "marketing"]);
});

test("a taken slug or name is a fault of the field the server names", () => {
  const err = new StructuredError("SLUG_TAKEN", "the project already has a design system with that name", 409, { field: "name" });
  assert.deepEqual(faultOf(err), { field: "name", code: "SLUG_TAKEN", message: "the project already has a design system with that name", next: "Choose another name." });
});

test("an invalid input names its field by the path of the first entry", () => {
  const err = new StructuredError("DESIGN_SYSTEM_INVALID", "invalid", 400, { errors: [{ path: "/slug", code: "BAD_NAME", message: "the slug must match ^[a-z][a-z0-9-]*$" }] });
  assert.deepEqual(faultOf(err), { field: "slug", code: "BAD_NAME", message: "the slug must match ^[a-z][a-z0-9-]*$", next: "Fix it and try again." });
});

test("any other refusal names no field", () => {
  assert.equal(faultOf(new StructuredError("NETWORK", "offline")).field, "");
});
