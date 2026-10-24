import { assert, file, test } from "../../../shared/testing/test.js";
import { isTextTarget, replaceField } from "./field.js";

file("writinghelper/presentation/field");

function textarea(value) {
  const el = document.createElement("textarea");
  el.value = value;
  document.body.append(el);
  return el;
}

test("replacing the field sets its text and fires input once for the form to follow", () => {
  const el = textarea("rough");
  const inputs = [];
  el.addEventListener("input", () => inputs.push(el.value));
  replaceField(el, "A clear brief.\n\nDone when: [open: x]");
  assert.equal(el.value, "A clear brief.\n\nDone when: [open: x]");
  assert.ok(inputs.length >= 1, "no input event");
  assert.equal(document.activeElement, el);
  el.remove();
});

test("replacing the field with nothing empties it", () => {
  const el = textarea("rough");
  replaceField(el, "");
  assert.equal(el.value, "");
  el.remove();
});

test("the browser's own undo brings the old text back", () => {
  const el = textarea("rough");
  replaceField(el, "A clear brief.");
  document.execCommand("undo");
  assert.equal(el.value, "rough");
  el.remove();
});

test("digit keys belong to text inputs and not to buttons or the page", () => {
  const input = document.createElement("input");
  const button = document.createElement("button");
  const box = document.createElement("input");
  box.type = "checkbox";
  assert.equal(isTextTarget(input), true);
  assert.equal(isTextTarget(textarea("")), true);
  assert.equal(isTextTarget(button), false);
  assert.equal(isTextTarget(box), false);
  assert.equal(isTextTarget(document.body), false);
});
