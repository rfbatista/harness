import { mount, seededElement } from "../../../../shared/testing/alpine.js";
import { flush } from "../../../../shared/testing/doubles.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { memoryArtifacts } from "../../infrastructure/memory-artifacts.js";
import { makeArtifact, toArtifactDTO } from "../../testing/artifact-fixtures.js";
import { designWatch } from "./designWatch.js";

file("sessions/presentation/designWatch");

const TICKETS = ["t1", "t2", "t3"].map((id) => ({ id, projectId: "p1" }));
const draft = () => makeArtifact({ id: "draft", ticketId: "t1", path: "d.html" });
const logo = (attached = ["t1"]) => makeArtifact({ id: "logo", ticketId: "t2", sessionId: "s2", path: "logo.svg", scope: "project", attachedTicketIds: attached });
const palette = () => makeArtifact({ id: "palette", ticketId: "t2", sessionId: "s2", path: "p.html", scope: "project" });

async function setup(world, seeded = world) {
  const memory = memoryArtifacts({ artifacts: world, tickets: TICKETS });
  const seed = { project_id: "p1", ticket_id: "t1", artifacts: seeded.map(toArtifactDTO) };
  const { instance } = mount(designWatch({ artifacts: memory.gateway }), { el: seededElement(seed) });
  instance.init();
  await flush();
  return { instance, memory };
}

test("counts what the task made and what is attached to it, from the seed", async () => {
  const { instance } = await setup([draft(), logo(), palette()]);
  assert.equal(instance.count, "2");
});

test("attaches, detaches, moves back and deletes change the count as they happen", async () => {
  const { instance, memory } = await setup([draft(), logo([]), palette()]);
  assert.equal(instance.count, "1");
  await memory.gateway.attach("palette", "t1");
  await flush();
  assert.equal(instance.count, "2");
  await memory.gateway.attach("palette", "t3");
  await flush();
  assert.equal(instance.count, "2", "an attach elsewhere changes nothing here");
  await memory.gateway.setScope("palette", "task");
  await flush();
  assert.equal(instance.count, "1", "moved back to its task: no longer attached here");
  await memory.gateway.setScope("draft", "project");
  await flush();
  assert.equal(instance.count, "1", "its own asset moving scope stays its own");
  await memory.gateway.remove("draft");
  await flush();
  assert.equal(instance.count, "0");
});

test("a seed without artifacts (an older server) counts none; unmounted, it stops following", async () => {
  const { instance, memory } = await setup([palette()], []);
  assert.equal(instance.count, "0");
  instance.destroy();
  await memory.gateway.attach("palette", "t1");
  await flush();
  assert.equal(instance.count, "0");
});
