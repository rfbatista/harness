import { assert, file, test } from "../../../shared/testing/test.js";
import { describeExport, describeRefusal, exportNodes, schemaChecks, yamlLines } from "./export.js";

file("architecture/domain/export");

test("each export state has its own words, and only the ones that need an action say what to do", () => {
  const titles = ["never_exported", "in_sync", "database_ahead", "edited_outside", "file_invalid"].map((s) => describeExport({ state: s }).title);
  assert.equal(new Set(titles).size, 5);
  assert.equal(describeExport({ state: "in_sync" }).next, "");
  assert.ok(describeExport({ state: "database_ahead" }).next.length > 0);
  assert.equal(describeExport({ state: "weird" }), null);
});

test("each refusal explains itself and what to do next", () => {
  for (const code of ["CHECKOUT_DIRTY", "WRONG_BRANCH", "ARCHITECTURE_CHANGED", "ARCHITECTURE_NOT_FOUND"]) {
    const r = describeRefusal({ code });
    assert.ok(r.title && r.text && r.next, code);
  }
  assert.equal(describeRefusal({ code: "NETWORK" }), null);
});

test("each export state reads as a banner: a tone and a one-line summary for the sync node", () => {
  const states = ["never_exported", "in_sync", "database_ahead", "edited_outside", "file_invalid"].map((s) => describeExport({ state: s }));
  for (const s of states) assert.ok(s.summary, s.title);
  assert.deepEqual(
    states.map((s) => s.tone),
    ["signal", "success", "signal", "attention", "danger"],
  );
});

test("the three nodes say what the database holds, how the two stand, and what the repository holds", () => {
  const n = exportNodes({ state: "database_ahead", path: ".harness/architecture.yaml", exported_version: 2, exported_commit: "a41c9e2f00", exported_at: "" }, { exists: true, version: 3 });
  assert.equal(n.database, "Saved version 3");
  assert.equal(n.repository, "Default branch · commit a41c9e2 (version 2)");
  assert.deepEqual(n.arrows, ["→", "←"]);
  const sync = exportNodes({ state: "in_sync", exported_version: 3, exported_commit: "a41c9e2f00" }, { exists: true, version: 3 });
  assert.deepEqual(sync.arrows, ["=", "="]);
  const never = exportNodes({ state: "never_exported", path: ".harness/architecture.yaml" }, { exists: true, version: 1 });
  assert.equal(never.repository, "No file at .harness/architecture.yaml yet");
  assert.equal(exportNodes(null, { exists: false, version: 0 }).database, "Nothing saved yet");
});

test("the schema checks say what was validated, and a file that is not valid says so", () => {
  const ok = schemaChecks({ state: "in_sync" }, { exists: true, version: 4, record: { version: 1 } });
  assert.equal(ok.badge, "Valid");
  assert.equal(ok.tone, "success");
  assert.ok(ok.items.some((i) => i.includes("version 4")));
  const bad = schemaChecks({ state: "file_invalid", path: ".harness/architecture.yaml" }, { exists: true, version: 4, record: { version: 1 } });
  assert.equal(bad.badge, "File invalid");
  assert.equal(bad.tone, "danger");
  assert.ok(bad.items.some((i) => i.includes(".harness/architecture.yaml")));
  assert.equal(schemaChecks(null, { exists: false, version: 0 }).badge, "Nothing saved");
});

test("the YAML is split into numbered lines whose pieces join back into the file", () => {
  const yaml = "# Generated\nversion: 1\nrules:\n  - id: a\n    message: 'x: y'\n";
  const lines = yamlLines(yaml);
  assert.equal(lines.length, 5);
  assert.equal(lines.map((l) => l.text).join(""), yaml);
  assert.equal(lines[0].comment, true);
  assert.equal(lines[1].key, "version");
  assert.equal(lines[3].lead, "  - ");
  assert.equal(lines[3].key, "id");
  assert.equal(lines[4].rest, ": 'x: y'\n");
  assert.deepEqual(yamlLines(""), []);
});
