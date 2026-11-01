import { StructuredError } from "../../../../shared/domain/errors.js";
import { mount } from "../../../../shared/testing/alpine.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { repositoryDesignSystem } from "./repositoryDesignSystem.js";

file("designsystem/presentation/components/repositoryDesignSystem");

const SYSTEMS = [
  { id: "ds1", name: "App", version: 12 },
  { id: "ds2", name: "Marketing", version: 4 },
];

/** A gateway the test plays by hand: what the server holds and the calls made. */
function fakeGateway({ systems = SYSTEMS, applied = null, path = ".harness/design-system/" } = {}) {
  const g = {
    state: { applied, path },
    calls: [],
    read: [],
    refuse: null,
    async designSystems() {
      return systems;
    },
    async repository(id) {
      g.read.push(id);
      return { id, designSystemId: g.state.applied ?? "", path: g.state.path };
    },
    async apply(repositoryId, designSystemId, path, confirmMove) {
      g.calls.push([repositoryId, designSystemId, path, confirmMove]);
      if (g.refuse) throw g.refuse;
      g.state = { applied: designSystemId ?? "", path: path || g.state.path };
      return { id: repositoryId, designSystemId: designSystemId ?? "", path: g.state.path };
    },
  };
  return g;
}

async function setup(options) {
  const gateway = fakeGateway(options);
  const el = document.createElement("div");
  el.dataset.projectId = "p1";
  el.dataset.repositoryId = "r1";
  const mounted = mount(repositoryDesignSystem({ gateway }), { el });
  await mounted.instance.init();
  return { ...mounted, gateway };
}

test("shows None and the design system the repository uses, with its path", async () => {
  const { instance } = await setup({ applied: "ds1", path: "ui/" });
  assert.deepEqual([instance.loaded, instance.selected, instance.appliedName, instance.draft, instance.showPath], [true, "ds1", "App", "ui/", true]);
  assert.deepEqual(instance.systems.map((s) => s.name), ["App", "Marketing"]);
});

test("a repository that uses none reads None and hides the path", async () => {
  const { instance } = await setup();
  assert.deepEqual([instance.selected, instance.showPath, instance.noSystems], ["", false, false]);
});

test("a project without design systems says so and cannot choose", async () => {
  const { instance } = await setup({ systems: [] });
  assert.deepEqual([instance.noSystems, instance.loaded], [true, true]);
});

test("choosing a design system applies it with the path as drafted when none is used", async () => {
  const { instance, gateway } = await setup();
  instance.draft = "ui/tokens/";
  await instance.choose("ds1");
  assert.deepEqual(gateway.calls, [["r1", "ds1", "ui/tokens/", false]]);
  assert.deepEqual([instance.applied, instance.saving, instance.error, instance.moveTo], ["ds1", false, null, null]);
});

test("choosing another one when one is used asks first and applies nothing", async () => {
  const { instance, gateway } = await setup({ applied: "ds1" });
  await instance.choose("ds2");
  assert.deepEqual(gateway.calls, []);
  assert.deepEqual([instance.moveOpen, instance.moveFromName, instance.moveToName], [true, "App", "Marketing"]);
});

test("confirming the move applies it with confirm_move", async () => {
  const { instance, gateway } = await setup({ applied: "ds1" });
  await instance.choose("ds2");
  await instance.confirmMove();
  assert.deepEqual(gateway.calls, [["r1", "ds2", ".harness/design-system/", true]]);
  assert.deepEqual([instance.applied, instance.selected, instance.moveOpen], ["ds2", "ds2", false]);
});

test("keeping the current one closes the question and goes back to it", async () => {
  const { instance, gateway } = await setup({ applied: "ds1" });
  await instance.choose("ds2");
  instance.keepCurrent();
  assert.deepEqual([instance.selected, instance.moveOpen, gateway.calls.length], ["ds1", false, 0]);
});

test("choosing None removes the design system at once, with no confirmation, and keeps the path", async () => {
  const { instance, gateway } = await setup({ applied: "ds1", path: "ui/" });
  await instance.choose("");
  assert.deepEqual(gateway.calls, [["r1", null, "ui/", false]]);
  assert.deepEqual([instance.applied, instance.showPath, instance.draft], ["", false, "ui/"]);
});

test("a refusal goes back to what was applied and shows its code", async () => {
  const { instance, gateway } = await setup();
  gateway.refuse = new StructuredError("INVALID_INPUT", "design system path must end in /", 400);
  instance.draft = "ui";
  await instance.choose("ds1");
  assert.deepEqual([instance.selected, instance.applied, instance.error.code], ["", "", "INVALID_INPUT"]);
});

test("a move the server still wants confirmed opens the question naming the current one", async () => {
  const { instance, gateway } = await setup();
  gateway.refuse = new StructuredError("MOVE_NOT_CONFIRMED", "already used", 409, { current: { design_system_id: "ds1", name: "App" } });
  await instance.choose("ds2");
  assert.deepEqual([instance.moveOpen, instance.moveFromName, instance.error], [true, "App", null]);
});

test("the path is saved on its own once it differs and a design system is chosen", async () => {
  const { instance, gateway } = await setup({ applied: "ds1" });
  assert.equal(instance.cannotSavePath, true);
  instance.draft = "ui/";
  assert.equal(instance.cannotSavePath, false);
  await instance.savePath();
  assert.deepEqual(gateway.calls, [["r1", "ds1", "ui/", false]]);
  assert.deepEqual([instance.path, instance.cannotSavePath], ["ui/", true]);
});

test("a refused path stays drafted and the saved one stays", async () => {
  const { instance, gateway } = await setup({ applied: "ds1" });
  gateway.refuse = new StructuredError("INVALID_INPUT", "design system path must stay inside the repository", 400);
  instance.draft = "../out/";
  await instance.savePath();
  assert.deepEqual([instance.applied, instance.path, instance.draft, instance.error.code], ["ds1", ".harness/design-system/", "../out/", "INVALID_INPUT"]);
});

test("a failed read shows the coded error and can be retried", async () => {
  const gateway = fakeGateway();
  gateway.designSystems = async () => {
    throw new StructuredError("INTERNAL", "boom", 500);
  };
  const el = document.createElement("div");
  el.dataset.projectId = "p1";
  el.dataset.repositoryId = "r1";
  const { instance } = mount(repositoryDesignSystem({ gateway }), { el });
  await instance.init();
  assert.deepEqual([instance.loadError.code, instance.loaded], ["INTERNAL", false]);
});

test("the repository is read by its own id", async () => {
  const { gateway } = await setup({ applied: "ds2" });
  assert.deepEqual(gateway.read, ["r1"]);
});
