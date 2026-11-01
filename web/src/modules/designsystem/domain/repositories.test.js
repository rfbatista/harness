import { assert, file, test } from "../../../shared/testing/test.js";
import { DEFAULT_PATH, chip, exportLabel, optInRow } from "./repositories.js";

file("designsystem/domain/repositories");

test("a repository row holds the saved opt-in and a draft of its path", () => {
  const row = optInRow({ repository_id: "r1", name: "spillway", design_system: { design_system_id: "ds1", name: "App" }, applied_here: true, path: "ui/tokens/", export: null });
  assert.deepEqual([row.id, row.name, row.uses, row.path, row.draft], ["r1", "spillway", true, "ui/tokens/", "ui/tokens/"]);
  assert.deepEqual([row.appliedId, row.appliedName], ["ds1", "App"]);
});

test("a row with no path shows the default one", () => {
  const row = optInRow({ repository_id: "r1", name: "docs" });
  assert.deepEqual([row.uses, row.path, row.draft], [false, DEFAULT_PATH, DEFAULT_PATH]);
  assert.equal(DEFAULT_PATH, ".harness/design-system/");
});

test("the export state is not exported until the export exists, and only for a repository that uses the design system", () => {
  assert.equal(exportLabel({ uses: true, export: null }), "Not exported yet");
  assert.equal(exportLabel({ uses: false, export: null }), "");
});

test("the chip says Applied here, which other design system it uses, or none", () => {
  assert.deepEqual(chip(optInRow({ repository_id: "r1", design_system: { design_system_id: "ds1", name: "App" }, applied_here: true })), { label: "Applied here", tone: "signal" });
  assert.deepEqual(chip(optInRow({ repository_id: "r2", design_system: { design_system_id: "ds2", name: "Brand" }, applied_here: false })), { label: "Uses Brand", tone: "" });
  assert.deepEqual(chip(optInRow({ repository_id: "r3" })), { label: "No design system", tone: "" });
});
