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
    async repositories(designSystemId) {
      g.asked.push(designSystemId);
      return g.server.map(optInRow);
    },
    async apply(id, designSystemId, path, confirmMove) {
      g.calls.push(confirmMove ? [id, designSystemId, path, "move"] : [id, designSystemId, path]);
      if (g.refuse) throw g.refuse;
      const d = g.server.find((x) => x.repository_id === id);
      if (designSystemId && d.design_system && d.design_system.design_system_id !== designSystemId && !confirmMove) {
        throw new StructuredError("MOVE_NOT_CONFIRMED", "uses another", 409, { current: d.design_system });
      }
      d.applied_here = designSystemId === "ds1";
      d.design_system = designSystemId ? here.design_system : null;
      if (path) d.path = path;
      return { id, designSystemId: designSystemId ?? "", path: d.path };
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

test("lists every repository of the project, with its chip", async () => {
  const { instance } = await setup([dto("api"), dto("web", { ...here, path: "ui/" }), dto("docs", other)]);
  assert.equal(instance.loaded, true);
  assert.deepEqual(instance.rows.map((r) => [r.name, r.uses, r.path]), [["api", false, ".harness/design-system/"], ["web", true, "ui/"], ["docs", false, ".harness/design-system/"]]);
  assert.deepEqual(instance.rows.map((r) => instance.chipOf(r).label), ["No design system", "Applied here", "Uses Brand"]);
  assert.equal(instance.isEmpty, false);
});

test("the rows are asked about the design system the page names", async () => {
  const { gateway } = await setup([dto("api")]);
  assert.deepEqual(gateway.asked, ["ds1"]);
});

test("a project without repositories says so", async () => {
  const { instance } = await setup([]);
  assert.equal(instance.isEmpty, true);
});

test("applying here sends this design system with the path as drafted", async () => {
  const { instance, gateway } = await setup([dto("api")]);
  const row = instance.rows[0];
  row.draft = "ui/tokens/";
  await instance.apply(row);
  assert.deepEqual(gateway.calls, [["api", "ds1", "ui/tokens/"]]);
  assert.deepEqual([row.uses, row.path, row.saving, row.error, instance.chipOf(row).label], [true, "ui/tokens/", false, null, "Applied here"]);
});

test("removing sends none with the saved path, so a half typed one is never sent", async () => {
  const { instance, gateway } = await setup([dto("api", { ...here, path: "ui/" })]);
  const row = instance.rows[0];
  row.draft = "ui/tok";
  await instance.remove(row);
  assert.deepEqual(gateway.calls, [["api", null, "ui/"]]);
  assert.deepEqual([row.uses, row.appliedId, instance.chipOf(row).label], [false, "", "No design system"]);
});

test("a refused apply says why, with its code, under the row, and changes nothing", async () => {
  const { instance, gateway } = await setup([dto("api")]);
  gateway.refuse = new StructuredError("INVALID_INPUT", "design system path must end in /", 400);
  const row = instance.rows[0];
  row.draft = "ui";
  await instance.apply(row);
  assert.deepEqual([row.uses, row.confirming], [false, false]);
  assert.equal(row.error.code, "INVALID_INPUT");
  assert.ok(/must end in \//.test(row.error.message));
  assert.equal(row.draft, "ui");
});

test("the path of an applied row is saved on its own once it differs", async () => {
  const { instance, gateway } = await setup([dto("api", { ...here })]);
  const row = instance.rows[0];
  assert.equal(instance.cannotSavePath(row), true);
  row.draft = "ui/";
  assert.equal(instance.cannotSavePath(row), false);
  await instance.savePath(row);
  assert.deepEqual(gateway.calls, [["api", "ds1", "ui/"]]);
  assert.deepEqual([row.path, instance.cannotSavePath(row)], ["ui/", true]);
});

test("the state beside a repository applied here is not exported yet; others have none", async () => {
  const { instance } = await setup([dto("api", { ...here }), dto("web"), dto("docs", other)]);
  assert.deepEqual(instance.rows.map((r) => instance.stateOf(r)), ["Not exported yet", "", ""]);
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

test("applying a repository that uses another design system asks to move it, naming the current one", async () => {
  const { instance, gateway } = await setup([dto("api", other)]);
  const row = instance.rows[0];
  await instance.apply(row);
  assert.deepEqual([row.confirming, row.movingFrom, row.uses, row.error], [true, "Brand", false, null]);
  assert.deepEqual(gateway.calls, [["api", "ds1", ".harness/design-system/"]]);
  assert.equal(row.saving, false);
});

test("confirming the move applies it here with the move confirmed", async () => {
  const { instance, gateway } = await setup([dto("api", other)]);
  const row = instance.rows[0];
  await instance.apply(row);
  await instance.confirmMove(row);
  assert.deepEqual(gateway.calls.at(-1), ["api", "ds1", ".harness/design-system/", "move"]);
  assert.deepEqual([row.confirming, row.uses, row.appliedId], [false, true, "ds1"]);
});

test("keeping the other design system closes the question and applies nothing", async () => {
  const { instance, gateway } = await setup([dto("api", other)]);
  const row = instance.rows[0];
  await instance.apply(row);
  instance.keepCurrent(row);
  assert.deepEqual([row.confirming, row.uses, gateway.calls.length], [false, false, 1]);
});
