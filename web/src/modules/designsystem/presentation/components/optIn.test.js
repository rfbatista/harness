import { StructuredError } from "../../../../shared/domain/errors.js";
import { mount } from "../../../../shared/testing/alpine.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { optInRow } from "../../domain/repositories.js";
import { designSystemOptIn } from "./optIn.js";

file("designsystem/presentation/components/optIn");

const dto = (id, over = {}) => ({ repository_id: id, name: id, design_system: null, applied_here: false, path: ".harness/design-system/", export: null, ...over });
const here = { design_system: { design_system_id: "ds1", name: "App" }, applied_here: true };
const other = { design_system: { design_system_id: "ds2", name: "Brand" }, applied_here: false };

/** A gateway the test plays by hand: what the server holds and the calls made. */
function fakeGateway(initial) {
  const g = {
    server: initial.map((d) => ({ ...d })),
    calls: [],
    refuse: null,
    asked: [],
    async repositories(projectId, designSystemId) {
      g.asked.push([projectId, designSystemId]);
      return g.server.map(optInRow);
    },
    async setRepository(id, uses, path, projectId, confirmMove, designSystemId) {
      g.calls.push(confirmMove ? [id, uses, path, "move"] : [id, uses, path]);
      g.applied = designSystemId;
      if (g.refuse) throw g.refuse;
      const d = g.server.find((x) => x.repository_id === id);
      d.applied_here = uses;
      d.design_system = uses ? here.design_system : null;
      if (path) d.path = path;
      return { id, uses, path: d.path, designSystemId: uses ? "ds1" : "" };
    },
  };
  return g;
}

async function setup(initial) {
  const gateway = fakeGateway(initial);
  const el = document.createElement("div");
  el.dataset.projectId = "p1";
  el.dataset.designSystemId = "ds1";
  const mounted = mount(designSystemOptIn({ gateway }), { el });
  await mounted.instance.init();
  return { ...mounted, gateway };
}

test("lists the project's repositories", async () => {
  const { instance } = await setup([dto("api"), dto("web", { ...here, path: "ui/" })]);
  assert.equal(instance.loaded, true);
  assert.deepEqual(instance.rows.map((r) => [r.name, r.uses, r.path]), [["api", false, ".harness/design-system/"], ["web", true, "ui/"]]);
  assert.equal(instance.isEmpty, false);
});

test("the rows are asked about the design system of the page, and switching on applies it", async () => {
  const { instance, gateway } = await setup([dto("api")]);
  assert.deepEqual(gateway.asked, [["p1", "ds1"]]);
  await instance.toggle(instance.rows[0]);
  assert.equal(gateway.applied, "ds1");
});

test("a project without repositories says so", async () => {
  const { instance } = await setup([]);
  assert.equal(instance.isEmpty, true);
});

test("switching on opts in with the path as drafted, and keeps it on", async () => {
  const { instance, gateway } = await setup([dto("api")]);
  const row = instance.rows[0];
  row.draft = "ui/tokens/";
  await instance.toggle(row);
  assert.deepEqual(gateway.calls, [["api", true, "ui/tokens/"]]);
  assert.deepEqual([row.uses, row.path, row.saving, row.error], [true, "ui/tokens/", false, null]);
});

test("switching off opts out and sends the path so it is kept", async () => {
  const { instance, gateway } = await setup([dto("api", { ...here, path: "ui/" })]);
  await instance.toggle(instance.rows[0]);
  assert.deepEqual(gateway.calls, [["api", false, "ui/"]]);
  assert.equal(instance.rows[0].uses, false);
});

test("switching off does not send a half typed path", async () => {
  const { instance, gateway } = await setup([dto("api", { ...here, path: "ui/" })]);
  const row = instance.rows[0];
  row.draft = "ui/tok";
  await instance.toggle(row);
  assert.deepEqual(gateway.calls, [["api", false, "ui/"]]);
});

test("a refused switch goes back and says why, with its code, under the row", async () => {
  const { instance, gateway } = await setup([dto("api")]);
  gateway.refuse = new StructuredError("INVALID_INPUT", "design system path must end in /", 400);
  const row = instance.rows[0];
  row.draft = "ui";
  await instance.toggle(row);
  assert.equal(row.uses, false);
  assert.equal(row.error.code, "INVALID_INPUT");
  assert.ok(/must end in \//.test(row.error.message));
});

test("the path is saved on its own once it differs, and only while the repository uses the design system", async () => {
  const { instance, gateway } = await setup([dto("api", { ...here })]);
  const row = instance.rows[0];
  assert.equal(instance.cannotSavePath(row), true);
  row.draft = "ui/";
  assert.equal(instance.cannotSavePath(row), false);
  await instance.savePath(row);
  assert.deepEqual(gateway.calls, [["api", true, "ui/"]]);
  assert.deepEqual([row.path, instance.cannotSavePath(row)], ["ui/", true]);
});

test("a refused path stays drafted and the saved one stays", async () => {
  const { instance, gateway } = await setup([dto("api", { ...here })]);
  const row = instance.rows[0];
  row.draft = "../out/";
  gateway.refuse = new StructuredError("INVALID_INPUT", "design system path must stay inside the repository", 400);
  await instance.savePath(row);
  assert.deepEqual([row.uses, row.path, row.draft], [true, ".harness/design-system/", "../out/"]);
  assert.equal(row.error.code, "INVALID_INPUT");
});

test("the state beside a repository that uses the design system is not exported yet", async () => {
  const { instance } = await setup([dto("api", { ...here }), dto("web")]);
  assert.deepEqual(instance.rows.map((r) => instance.stateOf(r)), ["Not exported yet", ""]);
});

test("a failed read shows the coded error and can be retried", async () => {
  const gateway = fakeGateway([dto("api")]);
  gateway.repositories = async () => {
    throw new StructuredError("INTERNAL", "boom", 500);
  };
  const el = document.createElement("div");
  el.dataset.projectId = "p1";
  el.dataset.designSystemId = "ds1";
  const { instance } = mount(designSystemOptIn({ gateway }), { el });
  await instance.init();
  assert.equal(instance.error.code, "INTERNAL");
  assert.equal(instance.loaded, false);
});

test("switching on a repository that uses another design system asks to move it, and applies nothing yet", async () => {
  const { instance, gateway } = await setup([dto("api", other)]);
  const row = instance.rows[0];
  assert.equal(instance.elsewhere(row), "Uses Brand now.");
  await instance.toggle(row);
  assert.deepEqual([row.confirming, row.uses, gateway.calls], [true, true, []]);
});

test("confirming the move applies it here with the move confirmed", async () => {
  const { instance, gateway } = await setup([dto("api", other)]);
  const row = instance.rows[0];
  await instance.toggle(row);
  await instance.confirmMove(row);
  assert.deepEqual(gateway.calls, [["api", true, ".harness/design-system/", "move"]]);
  assert.deepEqual([row.confirming, row.uses, row.appliedId], [false, true, "ds1"]);
});

test("keeping the other design system closes the question and leaves the switch off", async () => {
  const { instance, gateway } = await setup([dto("api", other)]);
  const row = instance.rows[0];
  await instance.toggle(row);
  instance.keepCurrent(row);
  assert.deepEqual([row.confirming, row.uses, gateway.calls], [false, false, []]);
});
