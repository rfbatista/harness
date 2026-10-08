import { mount } from "../../testing/alpine.js";
import { assert, file, test } from "../../testing/test.js";
import { ListboxSelect, dropdown } from "./dropdown.js";

file("shared/presentation/dropdown");

const env = (overrides = {}) => ({
  window: { innerWidth: 1200, innerHeight: 900, matchMedia: () => ({ matches: false }), addEventListener() {}, removeEventListener() {} },
  document,
  now: () => 0,
  setTimeout: (fn) => fn(),
  clearTimeout() {},
  ...overrides,
});

/** A wrapped select in the page; returns the component and a cleanup. */
function fixture(inner, { attrs = "", label = "Agent", e = env() } = {}) {
  const root = document.createElement("div");
  root.className = "dropdown";
  root.innerHTML = `<label for="dd-${++n}">${label}</label><select id="dd-${n}" ${attrs}>${inner}</select>`;
  const wrapper = document.createElement("div");
  wrapper.append(root);
  document.body.append(wrapper);
  const c = new ListboxSelect(root, e);
  return { c, root, select: root.querySelector("select"), trigger: c.trigger, done: () => (c.destroy(), wrapper.remove()) };
}
let n = 0;

const AGENTS = `
  <option value="">Plain claude</option>
  <option value="ux" data-description="Variants, wireframes">ux-designer</option>
  <option value="dev" data-meta="running" data-tone="signal">developer</option>
  <option value="old" disabled>retired</option>
  <option value="ops">operator</option>`;

const key = (el, k, init = {}) => {
  const e = new KeyboardEvent("keydown", { key: k, bubbles: true, cancelable: true, ...init });
  el.dispatchEvent(e);
  return e;
};
const activeLabel = (c) => c.rows[c.active]?.querySelector(".label").textContent;
const flush = () => new Promise((r) => setTimeout(r, 0));

test("hides the select and draws a trigger named by the label and the value", () => {
  const f = fixture(AGENTS);
  try {
    assert.ok(f.select.hidden);
    assert.equal(f.trigger.getAttribute("role"), "combobox");
    assert.equal(f.trigger.getAttribute("aria-expanded"), "false");
    assert.equal(f.trigger.querySelector(".value").textContent, "Plain claude");
    const [labelId, self] = f.trigger.getAttribute("aria-labelledby").split(" ");
    assert.equal(document.getElementById(labelId).textContent, "Agent");
    assert.equal(self, f.trigger.id);
    assert.equal(f.root.querySelector("label").htmlFor, f.trigger.id, "a click on the label focuses the trigger");
  } finally {
    f.done();
  }
});

test("↓ opens on the selected option; arrows skip disabled and do not wrap", () => {
  const f = fixture(AGENTS);
  try {
    f.select.value = "dev";
    f.c.sync();
    const e = key(f.trigger, "ArrowDown");
    assert.ok(e.defaultPrevented);
    assert.ok(f.c.isOpen);
    assert.equal(f.trigger.getAttribute("aria-expanded"), "true");
    assert.equal(activeLabel(f.c), "developer");
    assert.equal(f.trigger.getAttribute("aria-activedescendant"), f.c.rows[f.c.active].id);
    key(f.trigger, "ArrowDown");
    assert.equal(activeLabel(f.c), "operator", "retired is skipped");
    key(f.trigger, "ArrowDown");
    assert.equal(activeLabel(f.c), "operator", "no wrap");
    key(f.trigger, "Home");
    assert.equal(activeLabel(f.c), "Plain claude");
    key(f.trigger, "PageDown");
    assert.equal(activeLabel(f.c), "operator");
  } finally {
    f.done();
  }
});

test("Enter chooses: the select changes, `change` and dropdown-change fire", () => {
  const f = fixture(AGENTS);
  const changes = [];
  const picks = [];
  f.select.addEventListener("change", () => changes.push(f.select.value));
  f.root.addEventListener("dropdown-change", (e) => picks.push(e.detail));
  try {
    key(f.trigger, "Enter");
    key(f.trigger, "ArrowDown");
    key(f.trigger, "Enter");
    assert.ok(!f.c.isOpen);
    assert.equal(f.select.value, "ux");
    assert.deepEqual(changes, ["ux"]);
    assert.deepEqual(picks, [{ value: "ux", option: { value: "ux", label: "ux-designer" } }]);
    assert.equal(f.trigger.querySelector(".value").textContent, "ux-designer");
  } finally {
    f.done();
  }
});

test("Esc closes with no change and stops there", () => {
  const f = fixture(AGENTS);
  let outer = 0;
  f.root.parentElement.addEventListener("keydown", () => outer++);
  try {
    key(f.trigger, "Enter");
    key(f.trigger, "ArrowDown");
    key(f.trigger, "Escape");
    assert.ok(!f.c.isOpen);
    assert.equal(f.select.value, "");
    assert.equal(outer, 0, "the enclosing pane never sees the Esc");
  } finally {
    f.done();
  }
});

test("a closed trigger lets the app's keys through; an open one keeps them", () => {
  const f = fixture(AGENTS);
  let outer = 0;
  f.root.parentElement.addEventListener("keydown", () => outer++);
  try {
    key(f.trigger, "/", { ctrlKey: true });
    key(f.trigger, "Tab");
    assert.equal(outer, 2);
    key(f.trigger, "ArrowDown");
    key(f.trigger, "F2");
    assert.equal(outer, 2);
  } finally {
    f.done();
  }
});

test("Tab chooses the active option and lets the focus move on", () => {
  const f = fixture(AGENTS);
  try {
    key(f.trigger, "End");
    const e = key(f.trigger, "Tab");
    assert.ok(!e.defaultPrevented);
    assert.equal(f.select.value, "ops");
    assert.ok(!f.c.isOpen);
  } finally {
    f.done();
  }
});

test("a printable key opens and type-aheads", () => {
  const f = fixture(AGENTS);
  try {
    key(f.trigger, "o");
    assert.ok(f.c.isOpen);
    assert.equal(activeLabel(f.c), "operator");
  } finally {
    f.done();
  }
});

test("rows carry the option anatomy: description, dot and meta", () => {
  const f = fixture(AGENTS);
  try {
    f.c.open();
    const [, ux, dev, old] = f.c.rows;
    assert.equal(ux.querySelector(".description").textContent, "Variants, wireframes");
    assert.equal(dev.querySelector(".status").dataset.state, "running");
    assert.equal(dev.querySelector(".meta").textContent, "running");
    assert.equal(old.getAttribute("aria-disabled"), "true");
    old.click();
    assert.ok(f.c.isOpen, "a disabled option does nothing");
    dev.click();
    assert.equal(f.select.value, "dev");
  } finally {
    f.done();
  }
});

test("x-model-style writes to the select update the trigger", async () => {
  const f = fixture(AGENTS);
  try {
    f.select.options[4].selected = true; // what Alpine's x-model does
    await flush();
    assert.equal(f.trigger.querySelector(".value").textContent, "operator");
  } finally {
    f.done();
  }
});

test("live options keep the active option by value and never change the value", async () => {
  const f = fixture(`<option value="a">alpha</option><option value="b">beta</option><option value="c">gamma</option>`);
  try {
    f.select.value = "b";
    f.c.sync();
    f.c.open();
    key(f.trigger, "ArrowDown");
    assert.equal(activeLabel(f.c), "gamma");
    f.select.prepend(new Option("zero", "z"));
    await flush();
    assert.equal(activeLabel(f.c), "gamma", "same option, new index");
    f.select.querySelector('option[value="b"]').remove();
    await flush();
    assert.equal(f.trigger.querySelector(".value").textContent, "beta", "the gone value keeps its label");
  } finally {
    f.done();
  }
});

test("clearable: × and Delete clear to the placeholder", () => {
  const f = fixture(`<option value=""></option><option value="a">alpha</option>`, { label: "Owner" });
  f.root.dataset.clearable = "";
  f.root.dataset.placeholder = "Choose an owner…";
  try {
    f.select.value = "a";
    f.c.sync();
    f.c.open();
    assert.deepEqual(f.c.rows.map((r) => r.querySelector(".label").textContent), ["alpha"], "an empty placeholder option is no row");
    f.c.close();
    const clear = f.trigger.querySelector(".clear");
    assert.equal(clear.getAttribute("aria-label"), "Clear Owner");
    assert.equal(clear.tabIndex, -1);
    clear.click();
    assert.equal(f.select.value, "");
    assert.equal(f.trigger.querySelector(".placeholder").textContent, "Choose an owner…");
    f.select.value = "a";
    f.c.sync();
    key(f.trigger, "Delete");
    assert.equal(f.select.value, "");
  } finally {
    f.done();
  }
});

test("disabled, invalid and required carry over to the trigger", async () => {
  const f = fixture(AGENTS, { attrs: 'required aria-invalid="true" aria-describedby="err"' });
  try {
    assert.equal(f.trigger.getAttribute("aria-invalid"), "true");
    assert.equal(f.trigger.getAttribute("aria-required"), "true");
    assert.equal(f.trigger.getAttribute("aria-describedby"), "err");
    f.select.disabled = true;
    await flush();
    assert.equal(f.trigger.getAttribute("aria-disabled"), "true");
    assert.equal(f.trigger.tabIndex, -1);
    key(f.trigger, "ArrowDown");
    assert.ok(!f.c.isOpen);
  } finally {
    f.done();
  }
});

test("loading, empty and error with Retry", async () => {
  const f = fixture("", { label: "Agents" });
  const retries = [];
  f.root.addEventListener("dropdown-retry", () => retries.push(1));
  try {
    f.root.setAttribute("aria-busy", "true");
    f.c.open();
    assert.equal(f.c.listbox.getAttribute("aria-busy"), "true");
    assert.equal(f.c.listbox.querySelectorAll(".skeleton").length, 3);
    assert.equal(document.getElementById("dropdown-live").textContent, "Loading agents…");
    f.root.setAttribute("aria-busy", "false");
    f.root.dataset.emptyText = "No agents in this project yet.";
    await flush();
    assert.equal(f.c.popup.querySelector(".message").textContent, "No agents in this project yet.");
    f.root.dataset.error = "Could not load agents.";
    await flush();
    const message = f.c.popup.querySelector(".message");
    assert.equal(message.dataset.tone, "danger");
    message.querySelector("button").click();
    key(f.trigger, "Enter");
    assert.equal(retries.length, 2);
  } finally {
    f.done();
  }
});

test("on a phone it opens as a sheet with the label and Close, focus inside", () => {
  const f = fixture(AGENTS, { e: env({ window: { ...env().window, matchMedia: () => ({ matches: true }) } }) });
  try {
    f.trigger.focus();
    f.c.open();
    assert.ok(f.c.popup.hasAttribute("data-sheet"));
    const header = f.c.popup.querySelector("header");
    assert.equal(header.querySelector("span").textContent, "Agent");
    assert.equal(document.activeElement, f.c.listbox);
    assert.equal(f.c.listbox.getAttribute("aria-activedescendant"), f.c.rows[0].id);
    header.querySelector("button").click();
    assert.ok(!f.c.isOpen);
    assert.equal(document.activeElement, f.trigger, "focus returns to the trigger");
  } finally {
    f.done();
  }
});

test("Esc inside a dialog closes the dropdown, not the dialog", () => {
  const dialog = document.createElement("dialog");
  document.body.append(dialog);
  const root = document.createElement("div");
  root.innerHTML = `<select aria-label="Kind"><option>a</option><option>b</option></select>`;
  dialog.append(root);
  dialog.show();
  const c = new ListboxSelect(root, env());
  try {
    c.trigger.focus();
    key(c.trigger, "ArrowDown");
    key(c.trigger, "Escape");
    assert.ok(!c.isOpen);
    assert.ok(dialog.open, "the dialog stays open");
  } finally {
    c.destroy();
    dialog.remove();
  }
});

test("the Alpine component builds on init and restores the select on destroy", () => {
  const root = document.createElement("div");
  root.innerHTML = `<select aria-label="Kind"><option>a</option></select>`;
  document.body.append(root);
  const { instance } = mount(dropdown(env()), { el: root });
  instance.init();
  assert.ok(root.querySelector(".trigger"));
  instance.destroy();
  assert.ok(!root.querySelector(".trigger"));
  assert.ok(!root.querySelector("select").hidden);
  root.remove();
});

test("an x-model write while the list is open moves the check, keeps the active option, and never closes it", async () => {
  const f = fixture(`<option value="a">alpha</option><option value="b">beta</option><option value="c">gamma</option>`);
  try {
    f.c.open();
    key(f.trigger, "ArrowDown");
    assert.equal(activeLabel(f.c), "beta");
    f.select.options[2].selected = true; // Alpine's x-model, from elsewhere on the page
    await flush();
    assert.ok(f.c.isOpen);
    assert.equal(f.c.rows[2].getAttribute("aria-selected"), "true");
    assert.equal(f.c.rows[0].getAttribute("aria-selected"), "false");
    assert.equal(activeLabel(f.c), "beta", "the keys stay where they were");
    assert.equal(f.trigger.querySelector(".value").textContent, "gamma");
  } finally {
    f.done();
  }
});

test("a form reset puts the trigger back on the default option", async () => {
  const form = document.createElement("form");
  form.innerHTML = `<div class="dropdown"><select aria-label="Mode"><option value="">Default</option><option value="arch">Architect</option></select></div>`;
  document.body.append(form);
  const c = new ListboxSelect(form.querySelector(".dropdown"), env());
  try {
    c.choose(1);
    assert.equal(c.trigger.querySelector(".value").textContent, "Architect");
    form.reset();
    await new Promise((r) => setTimeout(r, 0));
    await flush();
    assert.equal(c.select.value, "");
    assert.equal(c.trigger.querySelector(".value").textContent, "Default");
  } finally {
    c.destroy();
    form.remove();
  }
});

test("destroy (x-if / x-for teardown) takes the accessor wrappers off the select", () => {
  const f = fixture(AGENTS);
  f.done();
  assert.ok(!Object.hasOwn(f.select, "value"));
  assert.ok(!Object.hasOwn(f.select, "selectedIndex"));
  assert.ok([...f.select.options].every((o) => !Object.hasOwn(o, "selected")));
});
