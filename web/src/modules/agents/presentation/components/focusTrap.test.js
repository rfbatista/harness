import { assert, file, test } from "../../../../shared/testing/test.js";
import { FOCUSABLE, trapTab } from "./focusTrap.js";

file("agents/presentation/components/focusTrap");

const control = (name, visible = true) => ({ name, focused: 0, focus() { this.focused++; }, checkVisibility: () => visible });
const press = (dialog, controls, doc, shiftKey = false, key = "Tab") => {
  const event = { key, shiftKey, currentTarget: { querySelectorAll: (selector) => (selector === FOCUSABLE ? controls : []) }, prevented: false, preventDefault() { this.prevented = true; } };
  trapTab(event, doc);
  return event;
};

test("Tab from the last control wraps to the first, and Shift+Tab from the first to the last", () => {
  const [first, middle, last] = [control("first"), control("middle"), control("last")];
  const controls = [first, middle, last];
  const forward = press(null, controls, { activeElement: last });
  assert.equal(forward.prevented, true);
  assert.equal(first.focused, 1);
  const back = press(null, controls, { activeElement: first }, true);
  assert.equal(back.prevented, true);
  assert.equal(last.focused, 1);
});

test("inside the dialog the browser moves the focus on its own", () => {
  const [first, middle, last] = [control("first"), control("middle"), control("last")];
  assert.equal(press(null, [first, middle, last], { activeElement: middle }).prevented, false);
  assert.equal(press(null, [first, middle, last], { activeElement: middle }, true).prevented, false);
});

test("a hidden control is not a stop: the last visible one wraps", () => {
  const [first, last, hidden] = [control("first"), control("last"), control("hidden", false)];
  const event = press(null, [first, last, hidden], { activeElement: last });
  assert.equal(event.prevented, true);
  assert.equal(first.focused, 1);
});

test("a focus outside the dialog is brought in at the first control, other keys are left alone", () => {
  const [first, last] = [control("first"), control("last")];
  assert.equal(press(null, [first, last], { activeElement: null }).prevented, true);
  assert.equal(first.focused, 1);
  assert.equal(press(null, [first, last], { activeElement: last }, false, "Enter").prevented, false);
  assert.equal(press(null, [], { activeElement: null }).prevented, false);
});
