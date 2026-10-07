import { Codes, StructuredError } from "../../../../shared/domain/errors.js";
import { FeedStatus } from "../../../../shared/domain/feed.js";
import { fixedClock } from "../../../../shared/infrastructure/clock.js";
import { mount, seededElement } from "../../../../shared/testing/alpine.js";
import { flush } from "../../../../shared/testing/doubles.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { memoryArtifacts } from "../../infrastructure/memory-artifacts.js";
import { A0, makeArtifact, toArtifactDTO } from "../../testing/artifact-fixtures.js";
import { taskDesign } from "./taskDesign.js";

file("sessions/presentation/taskDesign");

const TASKS = [
  { id: "t1", title: "Checkout", href: "/projects/p1/tasks/t1" },
  { id: "t2", title: "Brand refresh", href: "/projects/p1/tasks/t2" },
  { id: "t3", title: "Landing", href: "/projects/p1/tasks/t3" },
];
const SEED = { project_id: "p1", ticket_id: "t1", tasks: TASKS };
const TICKETS = TASKS.map((t) => ({ id: t.id, projectId: "p1" }));

const later = (s) => new Date(A0.getTime() + s * 1000);

/** Made in t1: a task-scope draft and a project asset. From t2: a logo attached to t1, and a palette that is not. */
const draft = () => makeArtifact({ id: "draft", title: "Form draft", ticketId: "t1", path: "form.html", updatedAt: later(1) });
const card = () => makeArtifact({ id: "card", title: "Pricing card", ticketId: "t1", path: "card.html", scope: "project", updatedAt: later(3) });
const logo = (attached = ["t1"]) =>
  makeArtifact({ id: "logo", title: "Logo", kind: "image", ticketId: "t2", sessionId: "s2", path: "logo.svg", scope: "project", attachedTicketIds: attached, updatedAt: later(5) });
const palette = () => makeArtifact({ id: "palette", title: "Palette", ticketId: "t2", sessionId: "s2", path: "palette.html", scope: "project", updatedAt: later(7) });

async function setup(artifacts, gatewayOverrides = {}, seed = SEED) {
  const memory = memoryArtifacts({ artifacts, tickets: TICKETS, now: () => later(60) });
  const gateway = { ...memory.gateway, ...gatewayOverrides };
  const timers = [];
  const mounted = mount(taskDesign({ artifacts: gateway, clock: fixedClock(later(60)), setTimeout: (fn) => timers.push(fn) }), { el: seededElement(seed) });
  await mounted.instance.init();
  await flush();
  return { ...mounted, memory, timers };
}

test("lists what the task made and what is attached to it, in two groups; the first made one is open", async () => {
  const { instance } = await setup([draft(), card(), logo(), palette()]);
  assert.deepEqual(instance.madeCards.map((c) => c.id), ["card", "draft"]);
  assert.deepEqual(instance.attachedCards.map((c) => [c.id, c.attachedMark, c.fromTitle, c.fromHref]), [["logo", "attached", "Brand refresh", "/projects/p1/tasks/t2"]]);
  assert.equal(instance.current.id, "card");
  assert.deepEqual([instance.countWord, instance.hasMade, instance.hasAttached, instance.isEmpty], ["3 assets", true, true, false]);
});

test("J/K walk the cards as shown: the made group, then the attached one", async () => {
  const { instance } = await setup([draft(), card(), logo()]);
  instance.next();
  assert.equal(instance.selectedId, "draft");
  instance.next();
  assert.equal(instance.selectedId, "logo", "the attached group comes after, though the logo is newest");
  instance.next();
  assert.equal(instance.selectedId, "logo");
  instance.previous();
  assert.equal(instance.selectedId, "draft");
});

test("seeded assets paint at once, without asking the server", async () => {
  let listed = 0;
  const seed = { ...SEED, artifacts: [toArtifactDTO(logo()), toArtifactDTO(palette())] };
  const { instance } = await setup([], { listTask: async () => (listed++, []) }, seed);
  assert.equal(listed, 0);
  assert.deepEqual(instance.cards.map((c) => c.id), ["logo"], "an asset not on this task is not shown, even if seeded");
});

test("with none it is empty", async () => {
  const { instance } = await setup([palette()]);
  assert.deepEqual([instance.isEmpty, instance.countWord, instance.hasCurrent], [true, "No assets", false]);
});

test("an attached asset offers Detach, not a scope move, and names where it came from", async () => {
  const { instance } = await setup([card(), logo()]);
  instance.select("logo");
  assert.deepEqual(
    [instance.currentIsAttached, instance.canMoveCurrent, instance.currentFromTitle, instance.hasCurrentFromHref, instance.detachAriaLabel],
    [true, false, "Brand refresh", true, "Detach Logo from this task"],
  );
  instance.select("card");
  assert.deepEqual([instance.currentIsAttached, instance.canMoveCurrent, instance.hasCurrentFromHref], [false, true, false]);
});

test("detaching takes the asset off this task's list, quietly", async () => {
  const { instance, memory } = await setup([card(), logo()]);
  instance.select("logo");
  await instance.detachCurrent();
  assert.deepEqual(instance.cards.map((c) => c.id), ["card"]);
  assert.equal(instance.current.id, "card");
  assert.equal(instance.announcement, "", "the person's own detach is no news");
  assert.deepEqual([...(await memory.gateway.listProject("p1")).find((a) => a.id === "logo").attachedTicketIds], [], "the asset stays in the project");
});

test("attach offers the project assets this task does not have; picking one adds and opens it", async () => {
  const { instance } = await setup([draft(), card(), logo(), palette()]);
  await instance.openAssetPicker();
  assert.equal(instance.pickerOpen, true);
  assert.deepEqual(instance.pickerResults.map((c) => [c.id, c.label, c.detail]), [["palette", "Palette", "page · from Brand refresh"]]);
  instance.pickerChoose();
  await flush();
  assert.deepEqual(instance.attachedCards.map((c) => c.id), ["palette", "logo"]);
  assert.equal(instance.current.id, "palette");
  assert.equal(instance.announcement, "");
});

test("with every project asset already here, the picker says so", async () => {
  const { instance } = await setup([card(), logo()]);
  await instance.openAssetPicker();
  assert.equal(instance.pickerIsEmpty, true);
  assert.ok(instance.pickerEmptyText.startsWith("This task already has every project asset."));
});

test("a library that will not load says why, and opens no picker", async () => {
  const down = async () => {
    throw new StructuredError(Codes.NETWORK, "The harness server is not reachable.");
  };
  const { instance } = await setup([card()], { listProject: down });
  await instance.openAssetPicker();
  assert.deepEqual([instance.pickerOpen, instance.error.code, instance.loadingLibrary], [false, Codes.NETWORK, false]);
});

test("moving a made asset to the project keeps it here; moving it back asks first when it is attached elsewhere", async () => {
  const { instance, memory } = await setup([draft(), makeArtifact({ id: "card", title: "Pricing card", ticketId: "t1", path: "card.html", scope: "project", attachedTicketIds: ["t3"], updatedAt: later(3) })]);
  instance.select("draft");
  await instance.move();
  assert.equal(instance.selected.scope, "project");
  assert.deepEqual(instance.madeCards.map((c) => c.id).sort(), ["card", "draft"]);

  instance.select("card");
  await instance.move();
  assert.deepEqual([instance.confirmingMoveBack, instance.moveBackQuestion], [true, "Move Pricing card back to Checkout? It will be detached from 1 task: Landing."]);
  instance.cancelMoveBack();
  assert.equal(instance.selected.scope, "project");
  await instance.move();
  await instance.confirmMoveBack();
  assert.deepEqual([instance.selected.scope, [...instance.selected.attachedTicketIds]], ["task", []]);
  assert.deepEqual(await memory.gateway.listTask("t3"), []);
});

test("others' changes arrive over the feed: attached here, detached from here, deleted", async () => {
  const { instance, memory } = await setup([card(), logo([]), palette()]);
  assert.deepEqual(instance.cards.map((c) => c.id), ["card"]);

  await memory.gateway.attach("palette", "t1");
  await flush();
  assert.deepEqual(instance.attachedCards.map((c) => c.id), ["palette"]);
  assert.equal(instance.announcement, "New here: Palette");
  assert.deepEqual(instance.freshIds, ["palette"]);

  await memory.gateway.attach("palette", "t3");
  await flush();
  assert.equal(instance.announcement, "Attached to Landing: Palette", "an attach elsewhere still changes the card");

  await memory.gateway.detach("palette", "t1");
  await flush();
  assert.deepEqual(instance.attachedCards, []);
  assert.equal(instance.announcement, "Detached from this task: Palette");

  await memory.gateway.attach("logo", "t3");
  await flush();
  assert.equal(instance.cards.length, 1, "an attach to another task is not this list's business");

  await memory.gateway.remove("card");
  await flush();
  assert.deepEqual([instance.isEmpty, instance.announcement], [true, "Deleted: Pricing card"]);
});

test("a move back of an attached asset by its own task drops it here", async () => {
  const { instance, memory } = await setup([card(), logo()]);
  await memory.gateway.setScope("logo", "task");
  await flush();
  assert.deepEqual(instance.cards.map((c) => c.id), ["card"]);
  assert.equal(instance.announcement, "Moved back to its task: Logo");
});

test("a failed detach reads as the server put it, and the list reloads", async () => {
  const refuse = async () => {
    throw new StructuredError(Codes.ARTIFACT_PRODUCER_TASK, "the producing task cannot be detached", 409);
  };
  const { instance } = await setup([card(), logo()], { detach: refuse });
  instance.select("logo");
  await instance.detachCurrent();
  assert.equal(instance.error.code, Codes.ARTIFACT_PRODUCER_TASK);
  assert.deepEqual([instance.madeCards.map((c) => c.id), instance.attachedCards.map((c) => c.id)], [["card"], ["logo"]], "nothing left the list");
  assert.equal(instance.currentPending, false);
});

test("a resync reloads the list; unmounted, it stops following", async () => {
  let listed = 0;
  const seed = { ...SEED, artifacts: [toArtifactDTO(card())] };
  const { instance, memory } = await setup([card(), logo()], {}, seed);
  const real = instance.load.bind(instance);
  instance.load = async () => (listed++, real());
  memory.feedStatus(FeedStatus.RESYNCED);
  await flush();
  assert.deepEqual([listed, instance.cards.length], [1, 2], "the logo, missed while away, is back");
  instance.destroy();
  await memory.gateway.attach("palette", "t1").catch(() => {});
  await flush();
  assert.equal(instance.cards.length, 2);
});
