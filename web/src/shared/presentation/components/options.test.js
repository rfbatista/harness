import { assert, file, test } from "../../testing/test.js";
import { edge, readOptions, renderOptions, step, watchValue } from "./options.js";

file("shared/presentation/options");

function selectOf(html) {
  const el = document.createElement("select");
  el.innerHTML = html;
  return el;
}

test("reads option data and groups from a select", () => {
  const select = selectOf(`
    <option value="" data-pinned>The checked-out branch</option>
    <optgroup label="Branches"><option value="main" data-meta="current" data-tone="signal">main</option></optgroup>
    <optgroup label="Remote" disabled><option value="o/x" data-description="from origin">o/x</option></optgroup>`);
  assert.deepEqual(readOptions(select), [
    { value: "", label: "The checked-out branch", pinned: true },
    { value: "main", label: "main", meta: "current", tone: "signal", group: "Branches" },
    { value: "o/x", label: "o/x", description: "from origin", group: "Remote", disabled: true },
  ]);
});

test("renders rows under group headings, with description, dot and match", () => {
  const listbox = document.createElement("div");
  const rows = renderOptions(
    listbox,
    [
      { value: "a", label: "alpha" },
      { value: "b", label: "beta", group: "B", description: "second", tone: "signal", meta: "running" },
    ],
    { idPrefix: "t", selected: new Set(["b"]), query: "et" },
  );
  assert.equal(rows.length, 2);
  assert.equal(rows[1].getAttribute("aria-selected"), "true");
  assert.equal(rows[1].querySelector("mark").textContent, "et");
  assert.equal(rows[1].querySelector(".status").dataset.state, "running");
  assert.equal(rows[1].getAttribute("aria-describedby"), "t-1-description");
  const group = listbox.querySelector('[role="group"]');
  assert.equal(group.querySelector(".group").textContent, "B");
  assert.equal(group.getAttribute("aria-labelledby"), group.querySelector(".group").id);
});

test("step clamps or wraps and skips disabled entries", () => {
  const items = [{}, { disabled: true }, {}, {}];
  assert.equal(step(items, 0, 1), 2);
  assert.equal(step(items, 3, 1), 3, "no wrap");
  assert.equal(step(items, 3, 1, { wrap: true }), 0);
  assert.equal(step(items, 2, -1), 0);
  assert.equal(step(items, 0, 10), 3);
  assert.equal(step(items, -1, 1), 0);
  assert.equal(edge(items, true), 3);
  assert.equal(step([{ disabled: true }], 0, 1), -1);
});

test("watchValue notices value, selectedIndex and option.selected writes", async () => {
  const select = selectOf(`<option value="a">a</option><option value="b">b</option>`);
  let calls = 0;
  const w = watchValue(select, () => calls++);
  select.value = "b";
  await Promise.resolve();
  assert.equal(calls, 1);
  assert.equal(select.value, "b", "the native setter still runs");
  select.options[0].selected = true;
  select.selectedIndex = 0;
  await Promise.resolve();
  assert.equal(calls, 2, "writes in one task notify once");
  select.append(new Option("c", "c"));
  w.track();
  select.options[2].selected = true;
  await Promise.resolve();
  assert.equal(calls, 3);
});
