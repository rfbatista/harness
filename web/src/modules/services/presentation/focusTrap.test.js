import { assert, file, test } from "../../../shared/testing/test.js";
import { trapTab } from "./focusTrap.js";

file("services/presentation/focusTrap");

const el = (name, log) => ({ name, focus: () => log.push(name), checkVisibility: () => true });

function dialog(log) {
  const items = [el("first", log), el("second", log), el("last", log)];
  return { items, currentTarget: { querySelectorAll: () => items } };
}
const key = (d, o) => ({ key: "Tab", shiftKey: false, preventDefault: () => (d.prevented = true), currentTarget: d.currentTarget, ...o });

test("Tab on the last control goes to the first", () => {
  const log = [];
  const d = dialog(log);
  trapTab(key(d), { activeElement: d.items[2] });
  assert.deepEqual(log, ["first"]);
});

test("Shift+Tab on the first control goes to the last", () => {
  const log = [];
  const d = dialog(log);
  trapTab(key(d, { shiftKey: true }), { activeElement: d.items[0] });
  assert.deepEqual(log, ["last"]);
});

test("Tab in the middle is left alone", () => {
  const log = [];
  const d = dialog(log);
  trapTab(key(d), { activeElement: d.items[1] });
  assert.deepEqual(log, []);
  assert.equal(d.prevented, undefined);
});
