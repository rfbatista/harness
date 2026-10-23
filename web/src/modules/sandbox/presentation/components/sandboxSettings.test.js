import { StructuredError } from "../../../../shared/domain/errors.js";
import { mount } from "../../../../shared/testing/alpine.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { sandboxSettings } from "./sandboxSettings.js";

file("sandbox/presentation/components/sandboxSettings");

const seed = { enabled: true, driver: "seatbelt", paths: [{ path: "/opt/cache", write: false }] };

/** The section as the server paints it: the seed, and a first-paint block. */
let sections = 0;
function section(state = seed) {
  const id = `sandbox-seed-test-${++sections}`;
  const el = document.createElement("section");
  el.dataset.seed = id;
  el.innerHTML = `<script type="application/json" id="${id}">${JSON.stringify(state)}</script><div data-ssr>first paint</div>`;
  document.body.append(el);
  return el;
}

/** A gateway that applies each change to a copy of state, as the server would. */
function fakeGateway(state = seed, { refuse = null } = {}) {
  let current = structuredClone(state);
  const calls = [];
  const answer = (call) => {
    calls.push(call);
    if (refuse) throw refuse;
    return structuredClone(current);
  };
  return {
    calls,
    setEnabled: async (on) => ((current.enabled = on), answer(["setEnabled", on])),
    addPath: async (path, write) => (current.paths.push({ path, write }), answer(["addPath", path, write])),
    setPathAccess: async (path, write) => ((current.paths.find((p) => p.path === path).write = write), answer(["setPathAccess", path, write])),
    removePath: async (path) => ((current.paths = current.paths.filter((p) => p.path !== path)), answer(["removePath", path])),
  };
}

function mounted(gateway = fakeGateway(), state = seed) {
  const el = section(state);
  const m = mount(sandboxSettings({ gateway }), { el });
  m.instance.init();
  return { ...m, el };
}

test("init takes the seed over and removes the first paint", () => {
  const { instance, el } = mounted();
  assert.equal(instance.enabled, true);
  assert.deepEqual(instance.rows.map((r) => [r.path, r.access]), [["/opt/cache", "read"]]);
  assert.equal(el.querySelector("[data-ssr]"), null);
});

test("turning the sandbox off asks first; Keep it on changes nothing", async () => {
  const gateway = fakeGateway();
  const { instance } = mounted(gateway);
  await instance.toggle();
  assert.equal(instance.confirming, true);
  assert.equal(instance.enabled, true);
  instance.keepOn();
  assert.equal(instance.confirming, false);
  assert.deepEqual(gateway.calls, []);
});

test("confirming switches it off, and switching it on again needs no confirmation", async () => {
  const gateway = fakeGateway();
  const { instance } = mounted(gateway);
  await instance.toggle();
  await instance.turnOff();
  assert.equal(instance.enabled, false);
  assert.equal(instance.confirming, false);
  await instance.toggle();
  assert.equal(instance.enabled, true);
  assert.deepEqual(gateway.calls, [["setEnabled", false], ["setEnabled", true]]);
});

test("a path is added with its access and the field is emptied", async () => {
  const gateway = fakeGateway();
  const { instance } = mounted(gateway);
  instance.newPath = "~/go/pkg/mod";
  instance.newAccess = "write";
  await instance.add();
  assert.deepEqual(gateway.calls, [["addPath", "~/go/pkg/mod", true]]);
  assert.equal(instance.newPath, "");
  assert.deepEqual(instance.rows.map((r) => [r.path, r.access]), [["/opt/cache", "read"], ["~/go/pkg/mod", "write"]]);
});

test("a refused path stays in the field with the reason", async () => {
  const gateway = fakeGateway(seed, { refuse: new StructuredError("INVALID_INPUT", "Use an absolute path or one starting with ~.") });
  const { instance } = mounted(gateway);
  instance.newPath = "projects/x";
  await instance.add();
  assert.equal(instance.newPath, "projects/x");
  assert.equal(instance.pathError, "Use an absolute path or one starting with ~.");
  assert.equal(instance.rows.length, 1);
  instance.editPath();
  assert.equal(instance.pathError, "");
});

test("access is switched and a path removed from its row", async () => {
  const gateway = fakeGateway();
  const { instance } = mounted(gateway);
  const row = document.createElement("li");
  row.dataset.path = "/opt/cache";
  const select = document.createElement("select");
  select.innerHTML = `<option value="read">r</option><option value="write">w</option>`;
  select.value = "write";
  row.append(select);
  await instance.setAccess({ target: select });
  assert.equal(instance.rows[0].access, "write");
  const button = document.createElement("button");
  row.append(button);
  await instance.remove({ currentTarget: button });
  assert.equal(instance.rows.length, 0);
  assert.equal(instance.isEmpty, true);
});

test("a path that does not exist yet, or is a credential folder, carries a warning and is still listed", async () => {
  const gateway = fakeGateway();
  const { instance } = mounted(gateway);
  gateway.addPath = async (path, write) => ({
    enabled: true,
    driver: "seatbelt",
    paths: [{ path: "~/.aws", write, warnings: ["missing", "credentials"] }, { path: "/opt/cache", write: false, warnings: [] }],
  });
  instance.newPath = "~/.aws";
  await instance.add();
  const [aws, cache] = instance.rows;
  assert.equal(aws.missingWarning, "This folder does not exist yet. It will apply once it does.");
  assert.equal(aws.credentialsWarning, "This folder usually holds credentials.");
  assert.equal(cache.missingWarning, "");
  assert.equal(cache.credentialsWarning, "");
  assert.equal(instance.pathError, "");
});

test("a path from an Allow this path link is in the field, not saved", () => {
  const gateway = fakeGateway();
  const { instance } = mounted(gateway, { ...seed, allowPath: "/Users/me/.cache/pnpm" });
  assert.equal(instance.newPath, "/Users/me/.cache/pnpm");
  assert.equal(instance.prefilled, true);
  assert.deepEqual(gateway.calls, []);
  assert.equal(instance.rows.length, 1);
});

test("the summary says on, off or no driver", () => {
  assert.deepEqual(mounted().instance.summary, { text: "Sandbox: on", tone: "signal", blocks: false });
  assert.deepEqual(mounted(fakeGateway(), { ...seed, enabled: false }).instance.summary, { text: "Sandbox: off", tone: "attention", blocks: false });
  assert.deepEqual(mounted(fakeGateway(), { ...seed, driver: "" }).instance.summary, { text: "Sandbox: no driver", tone: "danger", blocks: true });
});

test("a platform without a driver is named", () => {
  assert.equal(mounted(fakeGateway(), { ...seed, driver: "" }).instance.noDriver, true);
  assert.equal(mounted().instance.noDriver, false);
});
