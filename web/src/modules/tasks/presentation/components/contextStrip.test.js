import { mount } from "../../../../shared/testing/alpine.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { contextStrip } from "./contextStrip.js";

file("tasks/presentation/contextStrip");

function setup({ open = "", stored = "", hash = "" } = {}) {
  const el = document.createElement("div");
  el.dataset.open = open;
  el.dataset.stored = stored;
  const written = [];
  const prefs = { open: () => "", setOpen: (key) => written.push(key) };
  const mounted = mount(contextStrip({ prefs, hash: () => hash }), { el });
  mounted.instance.init();
  return { ...mounted, written };
}

test("opens the panel the server painted open", () => {
  assert.equal(setup({ open: "feature" }).instance.expanded("feature"), true);
  assert.equal(setup({ open: "" }).instance.expanded("feature"), false);
});

test("opening a band closes the one that was open, and pressing the open band closes it", () => {
  const { instance } = setup();
  instance.toggle("feature");
  assert.deepEqual([instance.expanded("feature"), instance.expanded("reviews")], [true, false]);
  instance.toggle("reviews");
  assert.deepEqual([instance.expanded("feature"), instance.expanded("reviews")], [false, true]);
  instance.toggle("reviews");
  assert.equal(instance.expanded("reviews"), false);
});

test("a press is remembered as the panel's key, or none when it closed the panel", () => {
  const { instance, written } = setup();
  instance.toggle("feature");
  instance.toggle("feature");
  instance.toggle("token-savers");
  assert.deepEqual(written, ["feature", "none", "token-savers"]);
});

test("a link to a band opens it for the visit and remembers nothing", () => {
  const { instance, written } = setup({ open: "feature", hash: "#review-rv1" });
  assert.deepEqual([instance.expanded("reviews"), instance.expanded("feature")], [true, false]);
  assert.deepEqual(written, []);
});

test("a stored Reviews whose requests were only settled opens once the list is read, unless the person pressed something", () => {
  const read = (has) => ({ detail: { has } });
  const stored = setup({ open: "", stored: "reviews" });
  stored.instance.reviewsRead(read(true));
  assert.equal(stored.instance.expanded("reviews"), true);
  assert.deepEqual(stored.written, [], "not a press: nothing is written");

  const none = setup({ open: "", stored: "reviews" });
  none.instance.reviewsRead(read(false));
  assert.equal(none.instance.expanded("reviews"), false);

  const touched = setup({ open: "", stored: "reviews" });
  touched.instance.toggle("feature");
  touched.instance.reviewsRead(read(true));
  assert.equal(touched.instance.expanded("feature"), true);

  const other = setup({ open: "", stored: "feature" });
  other.instance.reviewsRead(read(true));
  assert.equal(other.instance.expanded("reviews"), false);
});

function keyEvent(key, target) {
  const calls = [];
  return { key, target, preventDefault: () => calls.push("prevented"), calls };
}

function stripWith(...bands) {
  const root = document.createElement("div");
  for (const band of bands) {
    const button = document.createElement("button");
    button.className = "cs-btn";
    button.dataset.band = band;
    root.append(button);
  }
  root.dataset.open = "";
  root.dataset.stored = "";
  document.body.append(root);
  const written = [];
  const mounted = mount(contextStrip({ prefs: { open: () => "", setOpen: (k) => written.push(k) }, hash: () => "" }), { el: root });
  mounted.instance.init();
  return { ...mounted, root, written, buttons: [...root.querySelectorAll(".cs-btn")] };
}

test("Escape closes the open panel, remembers none and returns focus to its button", () => {
  const { instance, buttons, written, root } = stripWith("reviews", "feature");
  instance.toggle("feature");
  buttons[0].focus();
  instance.key(keyEvent("Escape", document.body));
  assert.deepEqual([instance.expanded("feature"), written.at(-1), document.activeElement === buttons[1]], [false, "none", true]);
  root.remove();
});

test("Escape in a field is the field's, and with nothing open it does nothing", () => {
  const { instance, written, root } = stripWith("feature");
  const field = document.createElement("textarea");
  root.append(field);
  instance.toggle("feature");
  instance.key(keyEvent("Escape", field));
  assert.equal(instance.expanded("feature"), true);
  instance.toggle("feature");
  const before = written.length;
  instance.key(keyEvent("Escape", document.body));
  assert.equal(written.length, before);
  root.remove();
});

test("the arrow keys move focus along the visible buttons without opening one", () => {
  const { instance, buttons, root } = stripWith("reviews", "feature", "token-savers");
  buttons[1].hidden = true;
  buttons[0].focus();
  const right = keyEvent("ArrowRight", buttons[0]);
  instance.key(right);
  assert.deepEqual([document.activeElement === buttons[2], right.calls, instance.open], [true, ["prevented"], ""]);
  instance.key(keyEvent("Home", buttons[2]));
  assert.equal(document.activeElement, buttons[0]);
  root.remove();
});
