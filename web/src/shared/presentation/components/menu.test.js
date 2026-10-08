import { mount } from "../../testing/alpine.js";
import { assert, file, test } from "../../testing/test.js";
import { MenuButton, menu } from "./menu.js";

file("shared/presentation/menu");

const env = (phone = false) => ({
  window: { innerWidth: 1200, innerHeight: 900, matchMedia: () => ({ matches: phone }), addEventListener() {}, removeEventListener() {} },
  document,
  now: () => 0,
  setTimeout: (fn) => fn(),
  clearTimeout() {},
});

function fixture({ phone = false } = {}) {
  const root = document.createElement("div");
  root.className = "menu";
  root.innerHTML = `
    <button type="button" class="button" aria-label="More actions for Add SSE feed">⋯</button>
    <div role="menu">
      <button type="button" role="menuitem" data-action="copy-link">Copy link <kbd>C</kbd></button>
      <button type="button" role="menuitem" data-action="open" aria-disabled="true">Open terminal <span class="meta">not running</span></button>
      <hr role="separator">
      <button type="button" role="menuitemradio" data-action="move" data-value="review" aria-checked="false">Review</button>
      <button type="button" role="menuitem" data-action="delete" data-tone="danger">Delete…</button>
    </div>`;
  const outer = document.createElement("div");
  outer.append(root);
  document.body.append(outer);
  const m = new MenuButton(root, env(phone));
  const picks = [];
  root.addEventListener("menu-select", (e) => picks.push(e.detail));
  return { m, root, outer, picks, trigger: m.trigger, done: () => (m.close(), outer.remove()) };
}

const key = (el, k) => {
  const e = new KeyboardEvent("keydown", { key: k, bubbles: true, cancelable: true });
  el.dispatchEvent(e);
  return e;
};
const focusedText = () => document.activeElement.textContent.trim();

test("the trigger names its menu; ↓ opens with the focus on the first item", () => {
  const f = fixture();
  try {
    assert.equal(f.trigger.getAttribute("aria-haspopup"), "menu");
    assert.equal(f.m.popup.getAttribute("aria-labelledby"), f.trigger.id);
    f.trigger.focus();
    key(f.trigger, "ArrowDown");
    assert.ok(f.m.isOpen);
    assert.equal(f.trigger.getAttribute("aria-expanded"), "true");
    assert.ok(focusedText().startsWith("Copy link"));
    assert.equal(document.activeElement.tabIndex, 0, "roving tabindex");
  } finally {
    f.done();
  }
});

test("↑ opens on the last item; arrows wrap; Home and End; type-ahead", () => {
  const f = fixture();
  try {
    key(f.trigger, "ArrowUp");
    assert.equal(focusedText(), "Delete…");
    key(document.activeElement, "ArrowDown");
    assert.ok(focusedText().startsWith("Copy link"), "wraps to the first");
    key(document.activeElement, "ArrowUp");
    assert.equal(focusedText(), "Delete…", "wraps to the last");
    key(document.activeElement, "Home");
    assert.ok(focusedText().startsWith("Copy link"));
    key(document.activeElement, "r");
    assert.equal(focusedText(), "Review");
  } finally {
    f.done();
  }
});

test("Enter runs the item: menu-select, closed, focus back on the trigger", () => {
  const f = fixture();
  try {
    key(f.trigger, "Enter");
    key(document.activeElement, "Enter");
    assert.deepEqual(f.picks, [{ action: "copy-link" }]);
    assert.ok(!f.m.isOpen);
    assert.equal(document.activeElement, f.trigger);
  } finally {
    f.done();
  }
});

test("a radio item sends its value; a disabled item stays focusable and does nothing", () => {
  const f = fixture();
  try {
    key(f.trigger, "Enter");
    key(document.activeElement, "ArrowDown");
    assert.ok(focusedText().startsWith("Open terminal"), "the disabled item can be found");
    key(document.activeElement, "Enter");
    assert.deepEqual(f.picks, []);
    assert.ok(f.m.isOpen);
    key(document.activeElement, "ArrowDown");
    key(document.activeElement, " ");
    assert.deepEqual(f.picks, [{ action: "move", value: "review" }]);
  } finally {
    f.done();
  }
});

test("an action that moves the focus keeps it there", () => {
  const f = fixture();
  const elsewhere = document.createElement("button");
  document.body.append(elsewhere);
  f.root.addEventListener("menu-select", () => elsewhere.focus());
  try {
    key(f.trigger, "Enter");
    document.activeElement.click();
    assert.equal(document.activeElement, elsewhere);
  } finally {
    elsewhere.remove();
    f.done();
  }
});

test("Esc closes and refocuses, and the keys stop at the menu; Tab closes and moves on", () => {
  const f = fixture();
  let outer = 0;
  f.outer.addEventListener("keydown", () => outer++);
  try {
    key(f.trigger, "Enter");
    key(document.activeElement, "Escape");
    assert.ok(!f.m.isOpen);
    assert.equal(document.activeElement, f.trigger);
    assert.equal(outer, 0);
    key(f.trigger, "Enter");
    const e = key(document.activeElement, "Tab");
    assert.ok(!e.defaultPrevented);
    assert.ok(!f.m.isOpen);
  } finally {
    f.done();
  }
});

test("a click on the trigger toggles; a closed menu lets the app's keys through", () => {
  const f = fixture();
  let outer = 0;
  f.outer.addEventListener("keydown", () => outer++);
  try {
    key(f.trigger, "j");
    assert.equal(outer, 1);
    f.trigger.click();
    assert.ok(f.m.isOpen);
    f.trigger.click();
    assert.ok(!f.m.isOpen);
  } finally {
    f.done();
  }
});

test("on a phone it is an action sheet titled with the object", () => {
  const f = fixture({ phone: true });
  try {
    f.trigger.click();
    assert.ok(f.m.popup.hasAttribute("data-sheet"));
    assert.equal(f.m.popup.querySelector("header").textContent, "More actions for Add SSE feed");
  } finally {
    f.done();
  }
});

test("the Alpine component wires the menu on init", () => {
  const root = document.createElement("div");
  root.innerHTML = `<button type="button">More</button><div role="menu"><button type="button" role="menuitem">A</button></div>`;
  document.body.append(root);
  const { instance } = mount(menu(env()), { el: root });
  instance.init();
  assert.equal(root.querySelector("button").getAttribute("aria-haspopup"), "menu");
  instance.destroy();
  root.remove();
});
