import { mount } from "../../testing/alpine.js";
import { assert, file, test } from "../../testing/test.js";
import { AnchoredDialog, popupDialog } from "./dialog.js";

file("shared/presentation/dialog");

const env = (phone = false) => ({
  window: { innerWidth: 1200, innerHeight: 900, matchMedia: () => ({ matches: phone }), addEventListener() {}, removeEventListener() {} },
  document,
  now: () => 0,
  setTimeout: (fn) => fn(),
  clearTimeout() {},
});

function fixture({ role = "alertdialog", trap = true, phone = false } = {}) {
  const outer = document.createElement("div");
  outer.innerHTML = `
    <button type="button" id="dlg-anchor" aria-haspopup="dialog">⋯</button>
    <button type="button" id="dlg-elsewhere">Elsewhere</button>
    <div class="popup" role="${role}" id="dlg"${trap ? " data-trap" : ""}>
      <p>Delete this session?</p>
      <button type="button" data-initial-focus>Keep it</button>
      <button type="button" id="dlg-last">Delete session</button>
    </div>`;
  document.body.append(outer);
  const el = outer.querySelector("#dlg");
  const anchor = outer.querySelector("#dlg-anchor");
  const d = new AnchoredDialog(el, env(phone));
  return { d, el, anchor, outer, done: () => (d.close({ refocus: false }), outer.remove()) };
}

const key = (el, k, init = {}) => {
  const e = new KeyboardEvent("keydown", { key: k, bubbles: true, cancelable: true, ...init });
  el.dispatchEvent(e);
  return e;
};

test("opens from its anchor with the focus on [data-initial-focus]; the anchor says it is expanded", () => {
  const f = fixture();
  try {
    f.anchor.focus();
    f.d.open(f.anchor);
    assert.ok(f.d.isOpen);
    assert.equal(document.activeElement.textContent, "Keep it");
    assert.equal(f.anchor.getAttribute("aria-expanded"), "true");
    assert.equal(f.el.hasAttribute("data-sheet"), false, "anchored on a wide screen");
  } finally {
    f.done();
  }
});

test("Esc closes it, returns the focus to the anchor, and goes no further", () => {
  const f = fixture();
  let reachedOutside = false;
  const outside = () => (reachedOutside = true);
  document.addEventListener("keydown", outside);
  try {
    f.d.open(f.anchor);
    const e = key(document.activeElement, "Escape");
    assert.equal(f.d.isOpen, false);
    assert.equal(document.activeElement, f.anchor);
    assert.equal(f.anchor.getAttribute("aria-expanded"), "false");
    assert.ok(e.defaultPrevented);
    assert.equal(reachedOutside, false, "Esc stops at the dialog: a terminal behind it never sees it");
  } finally {
    document.removeEventListener("keydown", outside);
    f.done();
  }
});

test("data-trap keeps Tab inside, both ways", () => {
  const f = fixture();
  try {
    f.d.open(f.anchor);
    document.querySelector("#dlg-last").focus();
    const forward = key(document.activeElement, "Tab");
    assert.ok(forward.defaultPrevented);
    assert.equal(document.activeElement.textContent, "Keep it", "Tab from the last wraps to the first");
    const back = key(document.activeElement, "Tab", { shiftKey: true });
    assert.ok(back.defaultPrevented);
    assert.equal(document.activeElement.id, "dlg-last", "Shift+Tab from the first wraps to the last");
  } finally {
    f.done();
  }
});

test("a dialog closes on a press outside it; an alertdialog does not", () => {
  const dialog = fixture({ role: "dialog", trap: false });
  try {
    dialog.d.open(dialog.anchor);
    dialog.outer.querySelector("#dlg-elsewhere").dispatchEvent(new PointerEvent("pointerdown", { bubbles: true }));
    assert.equal(dialog.d.isOpen, false);
  } finally {
    dialog.done();
  }
  const alert = fixture();
  try {
    alert.d.open(alert.anchor);
    alert.outer.querySelector("#dlg-elsewhere").dispatchEvent(new PointerEvent("pointerdown", { bubbles: true }));
    assert.ok(alert.d.isOpen, "only its buttons or Esc close an alertdialog");
  } finally {
    alert.done();
  }
});

test("on a phone it opens as a sheet", () => {
  const f = fixture({ phone: true });
  try {
    f.d.open(f.anchor);
    assert.ok(f.el.hasAttribute("data-sheet"));
  } finally {
    f.done();
  }
});

test("popupDialog opens on dialog-open for its own id only, and closes with close()", () => {
  const outer = document.createElement("div");
  outer.innerHTML = `<button type="button" id="pd-anchor">Checks</button><div class="popup" role="dialog" id="pd"><button type="button" data-initial-focus>Pause checks</button></div>`;
  document.body.append(outer);
  const el = outer.querySelector("#pd");
  const anchor = outer.querySelector("#pd-anchor");
  const { instance } = mount(popupDialog(env()), { el });
  try {
    instance.init();
    instance.openFrom({ detail: { id: "other", anchor } });
    assert.equal(instance.isOpen, false);
    instance.openFrom({ detail: { id: "pd", anchor } });
    assert.equal(instance.isOpen, true);
    assert.equal(document.activeElement.textContent, "Pause checks");
    instance.close();
    assert.equal(instance.isOpen, false);
    assert.equal(document.activeElement, anchor);
  } finally {
    instance.destroy();
    outer.remove();
  }
});

test("each popupDialog keeps its own dialog, even nested in a scope that holds others", () => {
  const instance = popupDialog(env())();
  assert.ok(Object.prototype.hasOwnProperty.call(instance, "_dialog"), "declared, so Alpine does not write it to the outer scope");
});

test("act tells the page which dialog a button belongs to, whatever element called it", () => {
  const outer = document.createElement("div");
  outer.innerHTML = `<div class="popup" role="dialog" id="pd-act"><button type="button">Copy session id</button></div>`;
  document.body.append(outer);
  const el = outer.querySelector("#pd-act");
  const dispatched = [];
  const instance = popupDialog(env())();
  try {
    Object.defineProperties(instance, { $el: { value: el, configurable: true }, $dispatch: { value: (name, detail) => dispatched.push([name, detail]) } });
    instance.init();
    // As Alpine calls it from the button: $el is the button, not the dialog.
    Object.defineProperty(instance, "$el", { value: el.querySelector("button") });
    instance.act("copy-id");
    assert.deepEqual(dispatched, [["dialog-action", { id: "pd-act", action: "copy-id" }]]);
  } finally {
    instance.destroy();
    outer.remove();
  }
});

test("it tells its owner when it closes, however it closed, and closes on a dismiss for its id", () => {
  const f = fixture({ role: "dialog", trap: false });
  try {
    let closed = 0;
    const d = new AnchoredDialog(f.el, env(), () => closed++);
    d.open(f.anchor);
    key(f.el, "Escape");
    assert.deepEqual([d.isOpen, closed], [false, 1]);
    d.close();
    assert.equal(closed, 1, "closing a closed dialog is not a close");
  } finally {
    f.done();
  }
});
