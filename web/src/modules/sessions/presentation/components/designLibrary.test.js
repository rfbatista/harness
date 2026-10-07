import { Codes, StructuredError } from "../../../../shared/domain/errors.js";
import { fixedClock } from "../../../../shared/infrastructure/clock.js";
import { mount, seededElement } from "../../../../shared/testing/alpine.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { memoryArtifacts } from "../../infrastructure/memory-artifacts.js";
import { A0, makeArtifact } from "../../testing/artifact-fixtures.js";
import { designLibrary } from "./designLibrary.js";

file("sessions/presentation/designLibrary");

const SEED = {
  project_id: "p1",
  tasks: [
    { id: "t1", title: "Pricing page", href: "/projects/p1/tasks/t1" },
    { id: "t2", title: "Brand refresh", href: "/projects/p1/tasks/t2" },
  ],
};

const later = (s) => new Date(A0.getTime() + s * 1000);

async function setup(artifacts, gatewayOverrides = {}) {
  const memory = memoryArtifacts({ artifacts, now: () => later(60) });
  const gateway = { ...memory.gateway, ...gatewayOverrides };
  const mounted = mount(designLibrary({ artifacts: gateway, clock: fixedClock(later(60)) }), { el: seededElement(SEED) });
  await mounted.instance.init();
  return { ...mounted, memory };
}

const logo = () => makeArtifact({ id: "logo", title: "Logo", path: "brand/logo.svg", kind: "image", ticketId: "t2", scope: "project", updatedAt: later(5) });
const card = () => makeArtifact({ id: "card", title: "Pricing card", ticketId: "t1", scope: "project", updatedAt: later(1) });

test("lists the project's assets only, newest first, each naming its task; the newest is open", async () => {
  const taskOnly = makeArtifact({ id: "draft", path: "d.html", scope: "task", updatedAt: later(9) });
  const elsewhere = makeArtifact({ id: "x", path: "x.html", projectId: "p2", scope: "project", updatedAt: later(9) });
  const { instance } = await setup([card(), logo(), taskOnly, elsewhere]);
  assert.deepEqual(instance.cards.map((c) => [c.id, c.taskTitle]), [["logo", "Brand refresh"], ["card", "Pricing page"]]);
  assert.deepEqual([instance.current.id, instance.currentTaskTitle, instance.currentTaskHref], ["logo", "Brand refresh", "/projects/p1/tasks/t2"]);
  assert.equal(instance.countWord, "2 assets");
  assert.equal(instance.isEmpty, false);
  assert.equal(instance.followsFeed, false, "the library follows no stream");
});

test("with none it is empty", async () => {
  const { instance } = await setup([]);
  assert.deepEqual([instance.isEmpty, instance.countWord, instance.hasCurrent], [true, "No assets", false]);
});

test("an asset whose task is gone still lists, under a word that says so", async () => {
  const { instance } = await setup([makeArtifact({ id: "orphan", ticketId: "t9", scope: "project" })]);
  assert.equal(instance.cards[0].taskTitle, "a deleted task");
  assert.equal(instance.currentTaskHref, "");
});

test("moving an asset back to its task drops it from the list and offers the way to it", async () => {
  const { instance, memory } = await setup([card(), logo()]);
  await instance.moveBack();
  assert.deepEqual(instance.cards.map((c) => c.id), ["card"]);
  assert.equal(instance.current.id, "card", "the next asset opens");
  assert.deepEqual([instance.hasMovedBack, instance.movedBackTitle, instance.movedBackTaskTitle, instance.movedBackHref], [true, "Logo", "Brand refresh", "/projects/p1/tasks/t2"]);
  assert.deepEqual((await memory.gateway.listProject("p1")).map((a) => a.id), ["card"]);
  instance.dismissMovedBack();
  assert.equal(instance.hasMovedBack, false);
});

test("delete asks first; kept, nothing happens; confirmed, the asset is gone", async () => {
  const { instance, memory } = await setup([card(), logo()]);
  instance.askDelete();
  assert.deepEqual([instance.confirmingDelete, instance.deleteQuestion], [true, "Delete Logo?"]);
  instance.cancelDelete();
  assert.equal(instance.confirmingDelete, false);
  assert.equal(instance.cards.length, 2);
  instance.askDelete();
  await instance.confirmDelete();
  assert.deepEqual(instance.cards.map((c) => c.id), ["card"]);
  assert.equal(instance.confirmingDelete, false);
  assert.deepEqual((await memory.gateway.listProject("p1")).map((a) => a.id), ["card"]);
});

test("a failed move or delete reads as the server put it and leaves the list alone", async () => {
  const gone = async () => {
    throw new StructuredError(Codes.ARTIFACT_NOT_FOUND, "artifact not found", 404);
  };
  const { instance } = await setup([logo()], { setScope: gone, remove: gone });
  await instance.moveBack();
  assert.equal(instance.error.code, Codes.ARTIFACT_NOT_FOUND);
  assert.equal(instance.hasMovedBack, false);
  instance.dismissError();
  instance.askDelete();
  await instance.confirmDelete();
  assert.equal(instance.error.code, Codes.ARTIFACT_NOT_FOUND);
  assert.equal(instance.cards.length, 1);
});

test("a failed load shows the coded error; reload tries again", async () => {
  let fail = true;
  const memory = memoryArtifacts({ artifacts: [logo()] });
  const gateway = {
    ...memory.gateway,
    listProject: async (id) => {
      if (fail) throw new StructuredError(Codes.NETWORK, "The harness server is not reachable.");
      return memory.gateway.listProject(id);
    },
  };
  const { instance } = mount(designLibrary({ artifacts: gateway, clock: fixedClock(A0) }), { el: seededElement(SEED) });
  await instance.init();
  assert.deepEqual([instance.ready, instance.error.code, instance.isEmpty], [true, Codes.NETWORK, true]);
  fail = false;
  await instance.reload();
  assert.deepEqual([instance.error, instance.cards.length], [null, 1]);
});
