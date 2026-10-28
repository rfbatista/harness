import { assert, file, test } from "../../../shared/testing/test.js";
import { pickerKey } from "./entryPicker.js";

file("agents/domain/entryPicker");

const keys = ["@laws", "lint", "tdd"];
const press = (key, o = {}) => pickerKey({ open: false, active: 0, query: "", chosen: ["x"], ...o }, key, keys);

test("arrow keys open the list and move through it, wrapping at the ends", () => {
  assert.deepEqual(press("ArrowDown"), { open: true, active: 0, handled: true });
  assert.deepEqual(press("ArrowDown", { open: true, active: 0 }), { open: true, active: 1, handled: true });
  assert.deepEqual(press("ArrowDown", { open: true, active: 2 }), { open: true, active: 0, handled: true });
  assert.deepEqual(press("ArrowUp", { open: true, active: 0 }), { open: true, active: 2, handled: true });
});

test("Enter adds the active option and keeps the list open for the next one", () => {
  const r = press("Enter", { open: true, active: 1 });
  assert.equal(r.add, "lint");
  assert.equal(r.open, true);
  assert.equal(r.handled, true);
  assert.equal(press("Enter").handled, false, "a closed list leaves Enter alone");
  assert.equal(pickerKey({ open: true, active: 0, query: "zz", chosen: [] }, "Enter", []).handled, true, "no options: Enter still does not submit");
  assert.equal(pickerKey({ open: true, active: 0, query: "zz", chosen: [] }, "Enter", []).add, undefined);
});

test("Backspace in an empty input removes the last chip; with text it is just a Backspace", () => {
  assert.equal(press("Backspace").removeLast, true);
  assert.equal(press("Backspace", { query: "li" }).handled, false);
  assert.equal(pickerKey({ open: false, active: 0, query: "", chosen: [] }, "Backspace", keys).handled, false);
});

test("Escape closes the list; other keys are not ours", () => {
  assert.deepEqual(press("Escape", { open: true, active: 2 }), { open: false, active: 0, handled: true });
  assert.equal(press("Escape").handled, false, "already closed: let the page have it");
  assert.equal(press("a").handled, false);
});
