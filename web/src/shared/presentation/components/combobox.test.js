import { mount } from "../../testing/alpine.js";
import { assert, file, test } from "../../testing/test.js";
import { Combobox, combobox } from "./combobox.js";

file("shared/presentation/combobox");

/** A clock the test moves by hand. */
function fakeTimers() {
  let now = 0;
  let next = 1;
  const timers = new Map();
  return {
    setTimeout: (fn, ms) => (timers.set(next, { fn, at: now + ms }), next++),
    clearTimeout: (id) => timers.delete(id),
    advance(ms) {
      now += ms;
      for (const [id, t] of [...timers]) {
        if (t.at <= now) {
          timers.delete(id);
          t.fn();
        }
      }
    },
    now: () => now,
  };
}

const env = (overrides = {}) => {
  const timers = fakeTimers();
  return {
    window: { innerWidth: 1200, innerHeight: 900, matchMedia: () => ({ matches: false }), addEventListener() {}, removeEventListener() {} },
    document,
    now: timers.now,
    setTimeout: timers.setTimeout,
    clearTimeout: timers.clearTimeout,
    timers,
    ...overrides,
  };
};

let n = 0;
function fixture(inner, { attrs = "", data = {}, label = "Branch off", source = null, e = env() } = {}) {
  const root = document.createElement("div");
  root.className = "dropdown";
  Object.assign(root.dataset, data);
  const id = `cb-${++n}`;
  root.innerHTML = source
    ? `<label for="${id}">${label}</label><input type="hidden" id="${id}" name="pick" ${attrs}>`
    : `<label for="${id}">${label}</label><select id="${id}" ${attrs}>${inner}</select>`;
  const wrapper = document.createElement("form");
  wrapper.append(root);
  document.body.append(wrapper);
  const c = new Combobox(root, { env: e, source });
  return { c, root, e, select: root.querySelector("select"), input: c.input, done: () => (c.destroy(), wrapper.remove()) };
}

const BRANCHES = `
  <option value="" data-pinned>The checked-out branch</option>
  <optgroup label="Branches"><option value="main">main</option><option value="agent/dropdown">agent/dropdown</option><option value="agent/docs">agent/docs</option></optgroup>
  <optgroup label="Remote branches"><option value="origin/release">origin/release</option></optgroup>`;

const key = (el, k, init = {}) => {
  const e = new KeyboardEvent("keydown", { key: k, bubbles: true, cancelable: true, ...init });
  el.dispatchEvent(e);
  return e;
};
const type = (input, text) => {
  input.value = text;
  input.dispatchEvent(new Event("input", { bubbles: true }));
};
const labels = (c) => c.rows.map((r) => r.querySelector(".label").textContent);
const activeLabel = (c) => c.rows[c.active]?.querySelector(".label").textContent;
const flush = () => new Promise((r) => setTimeout(r, 0));
/** Moves the focus out, and says so even when the test page has no window focus. */
const blurTo = (from, to) => {
  to.focus();
  from.dispatchEvent(new FocusEvent("focusout", { bubbles: true, relatedTarget: to }));
};

test("an input combobox over the select, showing the chosen label", () => {
  const f = fixture(BRANCHES);
  try {
    assert.ok(f.select.hidden);
    assert.equal(f.input.getAttribute("role"), "combobox");
    assert.equal(f.input.getAttribute("aria-autocomplete"), "list");
    assert.equal(f.input.value, "The checked-out branch");
    assert.equal(f.root.querySelector("label").htmlFor, f.input.id);
    assert.equal(f.c.toggle.getAttribute("aria-label"), "Show branch off");
  } finally {
    f.done();
  }
});

test("typing filters, highlights the match, keeps the pinned row and makes the first match active", () => {
  const f = fixture(BRANCHES);
  try {
    f.input.focus();
    type(f.input, "agent");
    assert.ok(f.c.isOpen);
    assert.deepEqual(labels(f.c), ["The checked-out branch", "agent/dropdown", "agent/docs"]);
    assert.equal(activeLabel(f.c), "agent/dropdown");
    assert.equal(f.c.rows[1].querySelector("mark").textContent, "agent");
    assert.equal(document.getElementById("dropdown-live").textContent, "2 options");
    assert.ok(f.c.listbox.querySelector('[role="group"] .group'), "groups stay");
  } finally {
    f.done();
  }
});

test("Enter chooses the active match; the select changes and dropdown-change fires", () => {
  const f = fixture(BRANCHES);
  const picks = [];
  f.root.addEventListener("dropdown-change", (e) => picks.push(e.detail));
  try {
    f.input.focus();
    type(f.input, "rel");
    key(f.input, "Enter");
    assert.ok(!f.c.isOpen);
    assert.equal(f.select.value, "origin/release");
    assert.equal(f.input.value, "origin/release");
    assert.deepEqual(picks, [{ value: "origin/release", values: ["origin/release"], option: { value: "origin/release", label: "origin/release" }, created: false }]);
  } finally {
    f.done();
  }
});

test("no results says so; Esc closes, then clears the text, and blur reverts it", () => {
  const f = fixture(BRANCHES);
  const outside = document.createElement("button");
  document.body.append(outside);
  try {
    f.input.focus();
    type(f.input, "zzz");
    assert.equal(f.c.popup.querySelector(".message").textContent, "Nothing matches “zzz”.");
    assert.deepEqual(labels(f.c), ["The checked-out branch"], "the pinned row stays");
    key(f.input, "Escape");
    assert.ok(!f.c.isOpen);
    key(f.input, "Escape");
    assert.equal(f.input.value, "");
    blurTo(f.input, outside);
    assert.equal(f.input.value, "The checked-out branch", "half-typed text never looks chosen");
    assert.equal(f.select.value, "");
  } finally {
    outside.remove();
    f.done();
  }
});

test("↓ opens on the chosen option; Alt+↓ opens without moving; arrows do not wrap", () => {
  const f = fixture(BRANCHES);
  try {
    f.select.value = "main";
    f.c.sync();
    key(f.input, "ArrowDown", { altKey: true });
    assert.ok(f.c.isOpen);
    assert.equal(f.c.active, -1);
    f.c.close();
    key(f.input, "ArrowDown");
    assert.equal(activeLabel(f.c), "main");
    key(f.input, "End");
    assert.equal(activeLabel(f.c), "main", "Home/End move the caret, not the option");
    key(f.input, "PageDown");
    assert.equal(activeLabel(f.c), "origin/release");
    key(f.input, "ArrowDown");
    assert.equal(activeLabel(f.c), "origin/release");
  } finally {
    f.done();
  }
});

test("Enter on a closed list is left to the form", () => {
  const f = fixture(BRANCHES);
  try {
    const e = key(f.input, "Enter");
    assert.ok(!e.defaultPrevented);
  } finally {
    f.done();
  }
});

test("free text: a final “Run …” row, created: true, and the select carries it", () => {
  const f = fixture(`<option value="server">server</option><option value="tests">tests</option>`, {
    data: { freeText: "", freeTextLabel: "Run “{q}”" },
    label: "Command",
  });
  const picks = [];
  f.root.addEventListener("dropdown-change", (e) => picks.push(e.detail));
  try {
    f.input.focus();
    type(f.input, "make air");
    assert.deepEqual(labels(f.c), ["Run “make air”"]);
    key(f.input, "Enter");
    assert.equal(f.select.value, "make air");
    assert.equal(f.input.value, "make air");
    assert.deepEqual(picks.at(-1), { value: "make air", values: ["make air"], option: { value: "make air", label: "make air" }, created: true });
    f.c.open();
    assert.deepEqual(labels(f.c), ["server", "tests"], "the typed value is no suggestion");
    f.c.choose(0);
    assert.equal(f.select.querySelector("option[data-created]"), null, "a saved choice drops it");
    assert.equal(picks.at(-1).created, false);
  } finally {
    f.done();
  }
});

test("free text commits on blur, and on Enter in a closed list before the form submits", () => {
  const f = fixture(`<option value="server">server</option>`, { data: { freeText: "" }, label: "Command" });
  const outside = document.createElement("button");
  document.body.append(outside);
  try {
    f.input.focus();
    type(f.input, "make lint");
    blurTo(f.input, outside);
    assert.equal(f.select.value, "make lint");
    f.input.focus();
    type(f.input, "make vet");
    key(f.input, "Escape");
    key(f.input, "Enter");
    assert.equal(f.select.value, "make vet");
  } finally {
    outside.remove();
    f.done();
  }
});

test("multiple: Enter toggles and stays open, chips with remove, Backspace walks back", () => {
  const f = fixture(`<option value="design">design</option><option value="web">web</option><option value="a11y">a11y</option>`, {
    attrs: "multiple",
    label: "Labels",
  });
  try {
    f.input.focus();
    assert.equal(f.c.listbox.getAttribute("aria-multiselectable"), "true");
    type(f.input, "des");
    key(f.input, "Enter");
    assert.ok(f.c.isOpen, "stays open");
    assert.equal(f.input.value, "", "the text clears");
    assert.equal(document.getElementById("dropdown-live").textContent, "design added");
    key(f.input, "ArrowDown");
    key(f.input, "Enter");
    assert.deepEqual([...f.select.selectedOptions].map((o) => o.value), ["design", "web"]);
    assert.equal(f.c.popup.querySelector("footer span").textContent, "2 selected");
    const chips = f.root.querySelectorAll(".chips .chip");
    assert.equal(chips.length, 2);
    assert.equal(chips[1].querySelector("button").getAttribute("aria-label"), "Remove web");
    assert.equal(f.c.chips.getAttribute("role"), "list");
    key(f.input, "Backspace");
    assert.ok(f.root.querySelectorAll(".chip")[1].hasAttribute("data-active"));
    key(f.input, "Backspace");
    assert.deepEqual([...f.select.selectedOptions].map((o) => o.value), ["design"]);
    assert.equal(document.getElementById("dropdown-live").textContent, "web removed");
    f.root.querySelector(".chip button").click();
    assert.deepEqual([...f.select.selectedOptions].map((o) => o.value), []);
  } finally {
    f.done();
  }
});

test("a selected option stays listed in multiple mode, so it can be toggled off", () => {
  const f = fixture(`<option value="a" selected>alpha</option><option value="b">beta</option>`, { attrs: "multiple", label: "Labels" });
  try {
    f.c.open();
    assert.deepEqual(labels(f.c), ["alpha", "beta"]);
    assert.equal(f.c.rows[0].getAttribute("aria-selected"), "true");
    f.c.choose(0);
    assert.deepEqual([...f.select.selectedOptions].map((o) => o.value), []);
  } finally {
    f.done();
  }
});

test("loading shows a spinner in the field and skeleton rows; error has Retry", async () => {
  const f = fixture(BRANCHES);
  const retries = [];
  f.root.addEventListener("dropdown-retry", () => retries.push(1));
  try {
    f.root.setAttribute("aria-busy", "true");
    await flush();
    assert.ok(f.root.querySelector(".trigger .spinner"));
    assert.ok(f.c.toggle.hidden);
    f.c.open();
    assert.equal(f.c.listbox.querySelectorAll(".skeleton").length, 3);
    f.root.removeAttribute("aria-busy");
    f.root.dataset.error = "Could not list the branches.";
    await flush();
    const message = f.c.popup.querySelector(".message");
    assert.equal(message.dataset.tone, "danger");
    message.querySelector("button").click();
    assert.equal(retries.length, 1);
  } finally {
    f.done();
  }
});

test("query source: debounced 150ms, aborted when the text changes, only the latest renders", async () => {
  const calls = [];
  const source = (q, signal) => {
    const call = { q, signal };
    calls.push(call);
    return new Promise((resolve) => (call.resolve = resolve));
  };
  const f = fixture("", { source, label: "Task" });
  try {
    f.input.focus();
    type(f.input, "ad");
    f.e.timers.advance(100);
    type(f.input, "add");
    f.e.timers.advance(149);
    assert.equal(calls.length, 0, "debounced");
    f.e.timers.advance(1);
    assert.deepEqual(calls.map((c) => c.q), ["add"]);
    assert.ok(f.root.querySelector(".spinner"));
    assert.equal(document.getElementById("dropdown-live").textContent, "Searching…");
    type(f.input, "add s");
    f.e.timers.advance(150);
    assert.ok(calls[0].signal.aborted, "the older request is aborted");
    calls[0].resolve([{ value: "old", label: "stale" }]);
    calls[1].resolve([{ value: "t1", label: "Add SSE feed", description: "In progress" }]);
    await flush();
    assert.deepEqual(labels(f.c), ["Add SSE feed"]);
    key(f.input, "Enter");
    const hidden = f.root.querySelector('input[type="hidden"]');
    assert.equal(hidden.value, "t1");
    assert.equal(f.input.value, "Add SSE feed");
  } finally {
    f.done();
  }
});

test("query source: a failure shows the error row and Retry asks again", async () => {
  let fail = true;
  const calls = [];
  const source = (q) => (calls.push(q), fail ? Promise.reject(new Error("boom")) : Promise.resolve([{ value: "x", label: "x" }]));
  const f = fixture("", { source, label: "Task" });
  try {
    f.input.focus();
    type(f.input, "x");
    f.e.timers.advance(150);
    await flush();
    const message = f.c.popup.querySelector(".message");
    assert.equal(message.dataset.tone, "danger");
    fail = false;
    message.querySelector("button").click();
    await flush();
    assert.deepEqual(labels(f.c), ["x"]);
  } finally {
    f.done();
  }
});

test("query source with multiple: one hidden input per value", () => {
  const source = () => Promise.resolve([]);
  const root = document.createElement("div");
  root.dataset.multiple = "";
  root.innerHTML = `<label for="qm">Labels</label><input type="hidden" id="qm" name="label">`;
  document.body.append(root);
  const c = new Combobox(root, { env: env(), source });
  try {
    c.commit(["a", "b"], { value: "b", label: "b" });
    assert.deepEqual([...root.querySelectorAll('input[name="label"]')].map((i) => i.value), ["a", "b"]);
  } finally {
    c.destroy();
    root.remove();
  }
});

test("on a phone it opens as a tall sheet with the input first and Done", () => {
  const f = fixture(BRANCHES, { e: env({ window: { ...env().window, matchMedia: () => ({ matches: true }) } }) });
  try {
    f.input.focus();
    key(f.input, "ArrowDown");
    assert.ok(f.c.popup.hasAttribute("data-sheet"));
    assert.ok(f.c.popup.hasAttribute("data-tall"));
    assert.equal(f.input.parentElement.className, "search");
    assert.equal(document.activeElement, f.input);
    f.c.popup.querySelector("header button").click();
    assert.ok(!f.c.isOpen);
    assert.equal(f.input.parentElement, f.c.field, "the input goes back to the field");
  } finally {
    f.done();
  }
});

test("the Alpine component resolves data-source on its scope", async () => {
  const root = document.createElement("div");
  root.dataset.source = "searchTasks";
  root.innerHTML = `<input type="hidden" aria-label="Task">`;
  document.body.append(root);
  const { instance } = mount(combobox(env()), { el: root });
  const asked = [];
  instance.searchTasks = (q) => (asked.push(q), Promise.resolve([]));
  instance.init();
  try {
    instance._combobox.input.value = "x";
    instance._combobox.dirty = true;
    await instance._combobox.load();
    assert.deepEqual(asked, ["x"]);
  } finally {
    instance.destroy();
    root.remove();
  }
});
