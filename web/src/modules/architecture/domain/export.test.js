import { assert, file, test } from "../../../shared/testing/test.js";
import { describeExport, describeRefusal } from "./export.js";

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
