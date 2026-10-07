import { Codes, StructuredError } from "../../../../shared/domain/errors.js";
import { FeedStatus } from "../../../../shared/domain/feed.js";
import { fixedClock } from "../../../../shared/infrastructure/clock.js";
import { mount, seededElement } from "../../../../shared/testing/alpine.js";
import { flush } from "../../../../shared/testing/doubles.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { memoryArtifacts } from "../../infrastructure/memory-artifacts.js";
import { A0, makeArtifact, toArtifactDTO } from "../../testing/artifact-fixtures.js";
import { designLibrary } from "./designLibrary.js";

file("sessions/presentation/designLibrary");

const SEED = {
  project_id: "p1",
  tasks: [
    { id: "t1", title: "Pricing page", href: "/projects/p1/tasks/t1" },
    { id: "t2", title: "Brand refresh", href: "/projects/p1/tasks/t2" },
    { id: "t3", title: "Checkout", href: "/projects/p1/tasks/t3" },
  ],
};

const TICKETS = SEED.tasks.map((t) => ({ id: t.id, projectId: "p1" }));

const later = (s) => new Date(A0.getTime() + s * 1000);

async function setup(artifacts, gatewayOverrides = {}, seed = SEED) {
  const memory = memoryArtifacts({ artifacts, tickets: TICKETS, now: () => later(60) });
  const gateway = { ...memory.gateway, ...gatewayOverrides };
  const timers = [];
  const mounted = mount(designLibrary({ artifacts: gateway, clock: fixedClock(later(60)), setTimeout: (fn) => timers.push(fn) }), { el: seededElement(seed) });
  await mounted.instance.init();
  await flush();
  return { ...mounted, memory, timers };
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
  assert.equal(instance.followsFeed, true, "the bar says whether the project feed is live");
  assert.equal(instance.feedWord, "live");
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

// ── attachments ─────────────────────────────────────────────────────────

test("seeded assets paint at once, without asking the server", async () => {
  let listed = 0;
  const seed = { ...SEED, artifacts: [toArtifactDTO(logo()), toArtifactDTO(makeArtifact({ id: "draft", path: "d.html", scope: "task" }))] };
  const { instance } = await setup([], { listProject: async () => (listed++, []) }, seed);
  assert.equal(listed, 0);
  assert.deepEqual(instance.cards.map((c) => c.id), ["logo"], "only project assets, even if a task one slipped in");
  assert.equal(instance.ready, true);
});

test("the selected asset names the tasks it is attached to, each with its detach words", async () => {
  const { instance } = await setup([logo(), makeArtifact({ id: "card", ticketId: "t1", scope: "project", attachedTicketIds: ["t2", "t3"], updatedAt: later(9) })]);
  assert.equal(instance.cards[0].attachedWord, "attached to 2 tasks");
  assert.deepEqual(
    instance.attachedTasks.map((t) => [t.title, t.href, t.detachLabel]),
    [
      ["Brand refresh", "/projects/p1/tasks/t2", "Detach Pricing card from Brand refresh"],
      ["Checkout", "/projects/p1/tasks/t3", "Detach Pricing card from Checkout"],
    ],
  );
  instance.select("logo");
  assert.equal(instance.hasAttachedTasks, false);
});

test("attach offers the project's other tasks; picking one attaches it, quietly, though the feed's echo came first", async () => {
  const { instance, memory } = await setup([logo()]);
  instance.openAttachPicker();
  assert.equal(instance.pickerOpen, true);
  assert.equal(instance.pickerLabel, "Attach Logo to a task");
  assert.deepEqual(instance.pickerResults.map((c) => c.label), ["Pricing page", "Checkout"], "not its own task");
  instance.pickerQuery = "check";
  instance.pickerFiltered();
  assert.deepEqual(instance.pickerResults.map((c) => [c.id, c.active]), [["t3", true]]);
  instance.pickerChoose();
  assert.equal(instance.pickerOpen, false);
  assert.equal(instance.currentPending, true, "its actions wait for the answer");
  await flush();
  assert.equal(instance.currentPending, false);
  assert.deepEqual([...instance.selected.attachedTicketIds], ["t3"]);
  assert.equal(instance.announcement, "", "the person's own attach is no news");
  assert.deepEqual(instance.freshIds, []);
  assert.deepEqual((await memory.gateway.listTask("t3")).map((a) => a.id), ["logo"]);
});

test("with every task already on it, the picker says so", async () => {
  const { instance } = await setup([logo()]);
  await instance.attachTo("logo", "t1");
  await instance.attachTo("logo", "t3");
  instance.openAttachPicker();
  assert.deepEqual([instance.pickerIsEmpty, instance.pickerEmptyText], [true, "Every task of this project already has this asset."]);
  instance.pickerChoose();
  assert.equal(instance.pickerOpen, true, "Enter on nothing picks nothing");
  instance.closePicker();
  assert.equal(instance.pickerOpen, false);
});

test("detach takes a task off the selected asset", async () => {
  const { instance, memory } = await setup([makeArtifact({ id: "logo", ticketId: "t2", scope: "project", attachedTicketIds: ["t1", "t3"] })]);
  await instance.detachTask("t1");
  assert.deepEqual(instance.attachedTasks.map((t) => t.id), ["t3"]);
  assert.deepEqual(await memory.gateway.listTask("t1"), []);
});

test("moving back an attached asset asks first, naming the tasks; kept, nothing moves", async () => {
  const { instance, memory } = await setup([makeArtifact({ id: "logo", title: "Logo", ticketId: "t2", scope: "project", attachedTicketIds: ["t1", "t3"] })]);
  await instance.moveBack();
  assert.equal(instance.confirmingMoveBack, true);
  assert.equal(instance.moveBackQuestion, "Move Logo back to Brand refresh? It will be detached from 2 tasks: Pricing page, Checkout.");
  instance.cancelMoveBack();
  assert.equal(instance.confirmingMoveBack, false);
  assert.equal((await memory.gateway.listProject("p1")).length, 1);
  await instance.moveBack();
  await instance.confirmMoveBack();
  assert.deepEqual([instance.cards.length, instance.hasMovedBack, instance.movedBackTaskTitle], [0, true, "Brand refresh"]);
  assert.deepEqual(await memory.gateway.listTask("t1"), [], "detached everywhere");
  assert.equal(instance.announcement, "", "the person's own move is no news");
});

test("delete says it also leaves the tasks it is attached to", async () => {
  const { instance } = await setup([makeArtifact({ id: "logo", ticketId: "t2", scope: "project", attachedTicketIds: ["t1", "t3"] })]);
  instance.askDelete();
  assert.equal(instance.deleteConsequence, "It is removed for good, from the project and from its task, and from the 2 tasks it is attached to.");
});

test("what others do arrives over the feed: a new asset, an attach, a move back, a delete", async () => {
  const { instance, memory, timers } = await setup([logo(), card()]);
  const draft = memory.publish({ sessionId: "s3", ticketId: "t3", kind: "page", title: "Checkout form", path: "form.html", mime: "text/html", sizeBytes: 1 });
  await memory.gateway.setScope(draft.id, "project");
  await flush();
  assert.deepEqual(instance.cards.map((c) => c.id), [draft.id, "logo", "card"]);
  assert.deepEqual([instance.announcement, instance.freshIds], ["New here: Checkout form", [draft.id]]);
  assert.equal(instance.current.id, "logo", "the open asset stays open");
  timers.forEach((fn) => fn());
  assert.deepEqual(instance.freshIds, []);

  await memory.gateway.attach("logo", "t1");
  await flush();
  assert.equal(instance.announcement, "Attached to Pricing page: Logo");
  assert.deepEqual(instance.attachedTasks.map((t) => t.id), ["t1"]);

  await memory.gateway.detach("logo", "t1");
  await flush();
  assert.equal(instance.announcement, "Detached from Pricing page: Logo");

  await memory.gateway.setScope("logo", "task");
  await flush();
  assert.equal(instance.announcement, "Moved back to its task: Logo");
  assert.equal(instance.current.id, "card", "the open asset left: its neighbour opens");
  assert.equal(instance.hasMovedBack, false, "the banner is for the person's own move");

  await memory.gateway.remove("card");
  await flush();
  assert.equal(instance.announcement, "Deleted: Pricing card");
  assert.deepEqual(instance.cards.map((c) => c.id), [draft.id]);
});

test("a failed attach or detach reads as the server put it, and the list reloads", async () => {
  const { instance } = await setup([logo()]);
  await instance.detachTask("t2");
  assert.equal(instance.error.code, Codes.ARTIFACT_PRODUCER_TASK);
  assert.equal(instance.error.next, "this task made the asset, so it cannot be detached; move it back to the task or delete it instead");
  assert.equal(instance.currentPending, false);
});

test("the feed's pause shows on the bar; a resync reloads the list", async () => {
  let listed = 0;
  const seed = { ...SEED, artifacts: [toArtifactDTO(logo())] };
  const { instance, memory } = await setup([logo()], { listProject: async () => (listed++, [logo(), card()]) }, seed);
  assert.equal(listed, 0, "seeded");
  memory.feedStatus(FeedStatus.PAUSED);
  assert.equal(instance.feedWord, "live updates paused · retrying");
  memory.feedStatus(FeedStatus.RESYNCED);
  await flush();
  assert.deepEqual([instance.feedWord, listed, instance.cards.length], ["live", 1, 2], "changes may have been missed: the list comes again");
});

test("unmounted, it stops following the feed", async () => {
  const { instance, memory } = await setup([logo()]);
  instance.destroy();
  await memory.gateway.attach("logo", "t1");
  await flush();
  assert.equal(instance.announcement, "");
});
