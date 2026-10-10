import { preferences } from "../../../shared/infrastructure/storage.js";
import { assert, file, test } from "../../../shared/testing/test.js";
import { DEFAULT_TEXT_SIZE, nextTextSize, screenPrefs } from "./screenPrefs.js";

file("sessions/presentation/screenPrefs");

function memoryStorage() {
  const items = new Map();
  return { getItem: (k) => items.get(k) ?? null, setItem: (k, v) => items.set(k, v), removeItem: (k) => items.delete(k), items };
}

test("text size: remembered when it is one of the steps, the default otherwise", () => {
  const storage = memoryStorage();
  const prefs = screenPrefs(preferences(storage));
  assert.equal(prefs.textSize(), DEFAULT_TEXT_SIZE);
  prefs.setTextSize(15);
  assert.equal(prefs.textSize(), 15);
  prefs.setTextSize(99);
  assert.equal(prefs.textSize(), 15, "not a step: ignored");
  prefs.setTextSize(13);
  assert.equal(storage.items.size, 0, "the default is not stored");
  storage.setItem("harness:terminal-text-size", "40");
  assert.equal(prefs.textSize(), DEFAULT_TEXT_SIZE, "a stored value that is not a step reads as the default");
});

test("focus mode: on or off, off by default; storage that throws leaves the defaults", () => {
  const prefs = screenPrefs(preferences(memoryStorage()));
  assert.equal(prefs.focus(), false);
  prefs.setFocus(true);
  assert.equal(prefs.focus(), true);
  prefs.setFocus(false);
  assert.equal(prefs.focus(), false);

  const broken = { getItem() { throw new Error("blocked"); }, setItem() { throw new Error("blocked"); }, removeItem() { throw new Error("blocked"); } };
  const fallback = screenPrefs(preferences(broken));
  fallback.setFocus(true);
  assert.deepEqual([fallback.focus(), fallback.textSize()], [false, DEFAULT_TEXT_SIZE]);
});

test("the phone sheet's Text size steps up and wraps", () => {
  assert.deepEqual([nextTextSize(12), nextTextSize(13), nextTextSize(15)], [13, 15, 12]);
});
