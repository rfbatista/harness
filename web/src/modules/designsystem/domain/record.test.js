import { assert, file, test } from "../../../shared/testing/test.js";
import {
  CHANGED,
  INVALID,
  TABS,
  changeText,
  elsewhereText,
  invalidEntries,
  saveBarText,
  sortErrors,
  stateBadge,
  tabForSlug,
} from "./record.js";

file("designsystem/domain/record");

test("the tab list is Tokens first, and an unknown tab opens Tokens", () => {
  assert.equal(TABS[0].slug, "tokens");
  assert.equal(tabForSlug("history").slug, "history");
  assert.equal(tabForSlug("nonsense").slug, "tokens");
  assert.equal(tabForSlug("").slug, "tokens");
});

test("the server's refusals are told apart by code", () => {
  assert.equal(CHANGED, "DESIGN_SYSTEM_CHANGED");
  assert.equal(INVALID, "DESIGN_SYSTEM_INVALID");
});

test("invalid entries keep path, code and message, and drop what has no path", () => {
  const details = { errors: [{ path: "/tokens/1/value", code: "BAD_VALUE", message: "the value is empty" }, { code: "X" }] };
  assert.deepEqual(invalidEntries(details), [{ path: "/tokens/1/value", code: "BAD_VALUE", message: "the value is empty" }]);
  assert.deepEqual(invalidEntries(undefined), []);
});

test("an error lands on its row and field by the token's position; the rest are page-level", () => {
  const rows = [{ id: "a" }, { id: "b" }];
  const sorted = sortErrors(
    [
      { path: "/tokens/1/value", code: "BAD_VALUE", message: "m1" },
      { path: "/tokens/0", code: "DUPLICATE_TOKEN", message: "m2" },
      { path: "/tokens/0/name", code: "BAD_NAME", message: "m3" },
      { path: "/tokens", code: "TOKEN_IN_USE", message: "m4" },
    ],
    rows,
  );
  assert.deepEqual(sorted.byRow.b, [{ field: "value", code: "BAD_VALUE", message: "m1" }]);
  assert.deepEqual(sorted.byRow.a, [
    { field: "row", code: "DUPLICATE_TOKEN", message: "m2" },
    { field: "name", code: "BAD_NAME", message: "m3" },
  ]);
  assert.deepEqual(sorted.other, [{ path: "/tokens", code: "TOKEN_IN_USE", message: "m4" }]);
});

test("the save bar says which version it saves as", () => {
  assert.equal(saveBarText(0), "Saves as version 1");
  assert.equal(saveBarText(7), "Saves as version 8");
});

test("what changed is said in words, or that nothing did", () => {
  assert.equal(changeText({ edited: 0, added: 0, removed: 0 }, false), "No unsaved changes");
  assert.equal(changeText({ edited: 1, added: 1, removed: 0 }, false), "Unsaved: 1 edited, 1 added");
  assert.equal(changeText({ edited: 0, added: 0, removed: 2 }, false), "Unsaved: 2 removed");
  assert.equal(changeText({ edited: 0, added: 0, removed: 0 }, true), "Unsaved: principles changed");
});

test("the elsewhere notice names the version", () => {
  assert.equal(elsewhereText(8), "Version 8 was saved elsewhere. Your edits are kept and were not saved.");
  assert.equal(elsewhereText(0), "A newer version was saved elsewhere. Your edits are kept and were not saved.");
});

test("the state badge says what the page holds", () => {
  assert.deepEqual(stateBadge({ exists: false, started: false, version: 0, dirty: false }), { label: "Not started", tone: "" });
  assert.deepEqual(stateBadge({ exists: false, started: true, version: 0, dirty: true }), { label: "Draft, not saved", tone: "attention" });
  assert.deepEqual(stateBadge({ exists: true, started: true, version: 7, dirty: false }), { label: "Version 7", tone: "success" });
  assert.deepEqual(stateBadge({ exists: true, started: true, version: 7, dirty: true }), { label: "Version 7 · unsaved changes", tone: "attention" });
});
