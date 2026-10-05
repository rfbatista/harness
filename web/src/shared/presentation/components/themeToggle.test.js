import { preferences } from "../../infrastructure/storage.js";
import { mount } from "../../testing/alpine.js";
import { assert, file, test } from "../../testing/test.js";
import { themeToggle } from "./themeToggle.js";

file("shared/presentation/themeToggle");

function memoryStorage(initial = {}) {
  const data = new Map(Object.entries(initial));
  return {
    getItem: (k) => data.get(k) ?? null,
    setItem: (k, v) => data.set(k, v),
    removeItem: (k) => data.delete(k),
    data,
  };
}

test("restores the saved theme and pins it on <html>", () => {
  const storage = memoryStorage({ "harness:theme": "dark" });
  const doc = document.implementation.createHTMLDocument("t");
  const { instance } = mount(themeToggle(preferences(storage)), { el: doc.body });
  instance.init();
  assert.ok(instance.isDark);
  assert.equal(doc.documentElement.dataset.theme, "dark");
});

test("system clears the pin and the saved choice", () => {
  const storage = memoryStorage();
  const doc = document.implementation.createHTMLDocument("t");
  const { instance } = mount(themeToggle(preferences(storage)), { el: doc.body });
  instance.init();
  instance.light();
  assert.equal(storage.data.get("harness:theme"), "light");
  instance.system();
  assert.equal(doc.documentElement.dataset.theme, undefined);
  assert.equal(storage.data.has("harness:theme"), false);
});

test("works when storage throws", () => {
  const broken = { getItem() { throw new Error("blocked"); }, setItem() { throw new Error("blocked"); } };
  const doc = document.implementation.createHTMLDocument("t");
  const { instance } = mount(themeToggle(preferences(broken)), { el: doc.body });
  instance.init();
  instance.dark();
  assert.equal(doc.documentElement.dataset.theme, "dark");
});
