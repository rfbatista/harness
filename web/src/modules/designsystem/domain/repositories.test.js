import { assert, file, test } from "../../../shared/testing/test.js";
import { DEFAULT_PATH, EFFECT_NOTE, exportLabel, optInRow } from "./repositories.js";

file("designsystem/domain/repositories");

test("a repository row holds the saved opt-in and a draft of its path", () => {
  const row = optInRow({ repository_id: "r1", name: "spillway", uses_design_system: true, path: "ui/tokens/", export: null });
  assert.deepEqual([row.id, row.name, row.uses, row.path, row.draft], ["r1", "spillway", true, "ui/tokens/", "ui/tokens/"]);
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

test("the effect of the switch is stated in the person's words", () => {
  assert.ok(/Nothing is written to the repository until you export/.test(EFFECT_NOTE));
});
