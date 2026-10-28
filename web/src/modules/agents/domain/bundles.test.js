import { StructuredError } from "../../../shared/domain/errors.js";
import { assert, file, test } from "../../../shared/testing/test.js";
import { bundleChanges, bundleDetail, bundleDraft, bundleFieldErrors, bundlePickerOptions, bundleRows, bundlesFooter, checkBundleDraft, cleanBundleName, renameImpact } from "./bundles.js";
import { describeRefusal } from "./refusals.js";

file("agents/domain/bundles");

const bundle = (name, entries, resolved, usedByAgents = [], usedByBundles = []) => ({ name, entries, resolvedSkills: resolved, usedByAgents, usedByBundles });

// Worked example: ux holds @laws and lint; laws holds two skills; nobody names scratch.
const catalog = {
  root: "/data/agents",
  agents: [{ name: "ux-designer" }, { name: "docs" }],
  bundles: [
    bundle("ux", ["@laws", "lint"], ["hicks-law", "fitts-law", "lint"], ["ux-designer", "docs"]),
    bundle("laws", ["hicks-law", "fitts-law"], ["hicks-law", "fitts-law"], [], ["ux"]),
    bundle("scratch", ["humanizer", "ghost"], ["humanizer"]),
  ],
  skills: ["lint", "hicks-law", "fitts-law", "humanizer", "tdd"].map((name) => ({ name })),
};

test("a bundle row shows @name, its entries, its size after nesting and who names it", () => {
  const rows = bundleRows(catalog);
  assert.deepEqual(rows.map((r) => r.name), ["@ux", "@laws", "@scratch"]);
  assert.equal(rows[0].key, "ux");
  assert.equal(rows[0].href, "/agents/bundles/ux");
  assert.equal(rows[0].description, "@laws, lint");
  assert.equal(rows[0].meta, "3 skills · 2 agents");
  assert.equal(rows[1].meta, "2 skills · 0 agents");
  assert.equal(rows[2].meta, "1 skill · unused");
});

test("the text filter matches a name or an entry; Unused and Holds bundles narrow the list", () => {
  const names = (o) => bundleRows(catalog, o).map((r) => r.key);
  assert.deepEqual(names({ query: "hicks" }), ["laws"]);
  assert.deepEqual(names({ query: "ux" }), ["ux"]);
  assert.deepEqual(names({ filter: "unused" }), ["scratch"]);
  assert.deepEqual(names({ filter: "nested" }), ["ux"]);
  assert.deepEqual(names({ filter: "all" }), ["ux", "laws", "scratch"]);
});

test("the footer names the source", () => {
  assert.equal(bundlesFooter(catalog), "agents.json › bundles · 3 bundles");
  assert.equal(bundlesFooter({ bundles: [catalog.bundles[0]] }), "agents.json › bundles · 1 bundle");
});

test("a bundle's detail lists what it contains, its size and who names it", () => {
  const d = bundleDetail(catalog, "ux");
  assert.equal(d.title, "@ux");
  assert.equal(d.path, "agents.json › bundles.ux");
  assert.equal(d.sizeLabel, "3 skills after nested bundles");
  assert.equal(d.entriesHeading, "Contains · 2");
  assert.deepEqual(d.entries.map((e) => [e.label, e.kind, e.href]), [["@laws", "bundle", "/agents/bundles/laws"], ["lint", "skill", "/agents/skills/lint"]]);
  assert.deepEqual(d.agents.map((a) => [a.name, a.href]), [["ux-designer", "/agents/ux-designer"], ["docs", "/agents/docs"]]);
  assert.equal(d.unused, false);
  assert.equal(d.editHref, "/agents/bundles/ux/edit");
  assert.equal(d.copyPath, "/data/agents/agents.json");
});

test("a bundle nothing names is unused; an entry the catalog lacks is marked missing", () => {
  const d = bundleDetail(catalog, "scratch");
  assert.equal(d.unused, true);
  assert.deepEqual(d.entries.map((e) => [e.label, e.missing]), [["humanizer", false], ["ghost", true]]);
  assert.deepEqual(bundleDetail(catalog, "laws").bundles.map((b) => [b.name, b.href]), [["@ux", "/agents/bundles/ux"]]);
  assert.equal(bundleDetail(catalog, "nope"), null);
});

test("renaming names every agent and bundle whose @name will be rewritten", () => {
  const i = renameImpact(catalog, "ux", "design");
  assert.equal(i.renamed, true);
  assert.deepEqual(i.agents, ["ux-designer", "docs"]);
  assert.deepEqual(i.bundles, []);
  assert.equal(i.text, "2 agents and 0 bundles (ux-designer, docs)");
  const j = renameImpact(catalog, "laws", "rules");
  assert.equal(j.text, "0 agents and 1 bundle (@ux)");
  assert.equal(renameImpact(catalog, "scratch", "tmp").text, "0 agents and 0 bundles (none)");
});

test("no rename, no banner: the same name, a new bundle, or a name still being typed", () => {
  assert.equal(renameImpact(catalog, "ux", "ux").renamed, false);
  assert.equal(renameImpact(catalog, "", "anything").renamed, false);
  assert.equal(renameImpact(catalog, "ux", "").renamed, false);
  assert.equal(renameImpact(catalog, "ghost", "other").renamed, false);
});

test("the draft starts from the bundle, strips a typed @, and says what is wrong with the name", () => {
  const d = bundleDraft(catalog, "ux");
  assert.deepEqual(d, { previousName: "ux", name: "ux", entries: ["@laws", "lint"] });
  assert.deepEqual(bundleDraft(catalog, ""), { previousName: "", name: "", entries: [] });
  assert.equal(cleanBundleName("@Ux"), "Ux");
});

test("validity: a slug name and at least one entry; the name problem is told once it is typed", () => {
  const v = (name, entries) => checkBundleDraft({ previousName: "", name, entries });
  assert.equal(v("ios-dev", ["tdd"]).valid, true);
  assert.equal(v("", ["tdd"]).valid, false);
  assert.equal(v("", ["tdd"]).nameProblem, "", "an empty name is not yet an error, Save is just off");
  assert.equal(v("iOS Dev", ["tdd"]).valid, false);
  assert.equal(v("iOS Dev", ["tdd"]).nameProblem, "Bundle name uses lowercase letters, digits and hyphens, and starts with a letter or digit.");
  assert.equal(v("ios-dev", []).valid, false);
  assert.equal(v("ios-dev", []).entriesProblem, "", "empty is not an error until the server says so");
});

test("changes counts the name and the entries that differ from what was read", () => {
  const c = (draft) => bundleChanges(catalog, draft);
  assert.equal(c({ previousName: "ux", name: "ux", entries: ["@laws", "lint"] }), 0);
  assert.equal(c({ previousName: "ux", name: "design", entries: ["@laws", "lint"] }), 1);
  assert.equal(c({ previousName: "ux", name: "design", entries: ["lint"] }), 2);
  assert.equal(c({ previousName: "ux", name: "ux", entries: ["lint", "@laws"] }), 1, "order is part of the bundle");
  assert.equal(c({ previousName: "", name: "", entries: [] }), 0);
  assert.equal(c({ previousName: "", name: "new", entries: [] }), 1);
});

test("the picker lists bundles first, then skills, never the bundle itself, and marks what is chosen", () => {
  const all = bundlePickerOptions(catalog, { query: "", chosen: ["lint"], exclude: "ux" });
  assert.deepEqual(all.options.map((o) => o.key), ["@laws", "@scratch", "fitts-law", "hicks-law", "humanizer", "lint", "tdd"]);
  assert.equal(all.options.find((o) => o.key === "lint").chosen, true);
  assert.equal(all.options[0].kind, "bundle");
  assert.equal(all.options[0].hint, "2 skills");
  const hit = bundlePickerOptions(catalog, { query: "law", chosen: [], exclude: "" });
  assert.deepEqual(hit.options.map((o) => o.key), ["@laws", "fitts-law", "hicks-law"]);
  assert.equal(hit.empty, false);
  assert.equal(bundlePickerOptions(catalog, { query: "zzz", chosen: [], exclude: "" }).empty, true);
});

test("the picker shows a page of options and says how many wait", () => {
  const big = { bundles: [], skills: Array.from({ length: 120 }, (_, i) => ({ name: `s-${String(i).padStart(3, "0")}` })) };
  const r = bundlePickerOptions(big, { query: "", chosen: [], exclude: "" });
  assert.equal(r.options.length, 50);
  assert.equal(r.hidden, 70);
  assert.equal(bundlePickerOptions(big, { query: "s-11", chosen: [], exclude: "" }).hidden, 0);
});

test("a refusal belongs to the field it is about", () => {
  const f = (code, details) => bundleFieldErrors(describeRefusal(new StructuredError(code, "boom", 400, details)));
  assert.deepEqual(f("BUNDLE_CYCLE"), { name: "", entries: "Bundles cannot include each other, directly or through another bundle.", form: "" });
  assert.equal(f("NAME_TAKEN", { suggestion: "ux-2" }).name, "That name is taken. Try ux-2.");
  assert.equal(f("INVALID_INPUT", { field: "name" }).name, "boom");
  assert.equal(f("INVALID_INPUT", { field: "entries" }).entries, "boom");
  assert.equal(f("UNKNOWN_REFERENCE", { names: ["ghost"] }).entries, "These do not exist: ghost.");
  assert.equal(f("CATALOG_BROKEN").form, "boom");
  assert.deepEqual(bundleFieldErrors(null), { name: "", entries: "", form: "" });
});
