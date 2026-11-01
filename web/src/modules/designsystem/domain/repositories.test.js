import { assert, file, test } from "../../../shared/testing/test.js";
import { DEFAULT_PATH, chip, exportBadge, exportLabel, optInRow } from "./repositories.js";

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

const exported = (over) => optInRow({ repository_id: "r1", name: "spillway", design_system: { design_system_id: "ds1", name: "App" }, applied_here: true, path: ".harness/design-system/", export: { state: "in_sync", exported_version: 9, current_version: 9, exported_commit: "3fa91c2d0e", exported_at: "2026-11-01T10:00:00Z", ...over } });

test("the badge names the four export states, and there is none for a repository that does not use the design system", () => {
  assert.deepEqual(["never_exported", "in_sync", "behind", "edited_outside"].map((state) => exportBadge(exported({ state })).label), ["Never exported", "In sync", "Behind", "Edited outside the harness"]);
  assert.equal(exportBadge(optInRow({ repository_id: "r2" })), null);
  assert.equal(exportBadge(optInRow({ repository_id: "r3", applied_here: true })), null);
});

test("the export line gives versions and commit, and for edited outside the files that differ", () => {
  const now = new Date("2026-11-01T10:30:00Z");
  assert.equal(exportLabel(exported({}), now), "version 9 exported 30m ago · commit 3fa91c2");
  assert.equal(exportLabel(exported({ state: "behind", current_version: 12 }), now), "exported version 9, current 12 · commit 3fa91c2");
  assert.equal(exportLabel(exported({ state: "edited_outside", changed_files: ["tokens.json"] }), now), "version 9 exported 30m ago · commit 3fa91c2 · tokens.json differ");
  assert.equal(exportLabel(exported({ state: "never_exported", exported_version: null }), now), "No files at .harness/design-system/ yet");
});
