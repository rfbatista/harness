import { assert, file, test } from "../../../shared/testing/test.js";
import { toKeyEvent } from "./terminal-keys.js";

file("sessions/infrastructure/terminal-keys");

const key = (k, mods = {}) => ({ key: k, shiftKey: false, altKey: false, ctrlKey: false, metaKey: false, ...mods });

test("printable keys travel as text, shift already applied", () => {
  assert.deepEqual(toKeyEvent(key("a")), { code: 97, text: "a" });
  assert.deepEqual(toKeyEvent(key("A", { shiftKey: true })), { code: 65, text: "A" });
  assert.deepEqual(toKeyEvent(key("é")), { code: 233, text: "é" });
  assert.deepEqual(toKeyEvent(key(" ")), { code: 32, text: " " });
});

test("special keys use ultraviolet's codes", () => {
  assert.deepEqual(toKeyEvent(key("Enter")), { code: 13 });
  assert.deepEqual(toKeyEvent(key("Backspace")), { code: 127 });
  assert.deepEqual(toKeyEvent(key("ArrowUp")), { code: 1114113 });
  assert.deepEqual(toKeyEvent(key("Tab", { shiftKey: true })), { code: 9, mod: 1 });
  assert.deepEqual(toKeyEvent(key("F1")), { code: 1114156 });
});

test("chords carry the base key and the modifiers", () => {
  assert.deepEqual(toKeyEvent(key("c", { ctrlKey: true })), { code: 99, mod: 4 });
  assert.deepEqual(toKeyEvent(key("B", { altKey: true, shiftKey: true })), { code: 98, mod: 3 });
});

test("the browser keeps Cmd shortcuts, lone modifiers and dead keys", () => {
  assert.equal(toKeyEvent(key("c", { metaKey: true })), null);
  assert.equal(toKeyEvent(key("Shift", { shiftKey: true })), null);
  assert.equal(toKeyEvent(key("Dead")), null);
});
