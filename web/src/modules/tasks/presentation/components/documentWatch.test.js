import { mount } from "../../../../shared/testing/alpine.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { documentSignature } from "../../domain/documents.js";
import { memoryTasks } from "../../infrastructure/memory-gateway.js";
import { documentWatch, POLL_MS } from "./documentWatch.js";

file("tasks/presentation/documentWatch");

const doc = (id, updatedAt = "2026-10-05T12:00:00Z") => ({ id, ticketId: "t1", updatedAt });

function setup({ visible = true } = {}) {
  const memory = memoryTasks({ projects: ["p1"], documents: [doc("d1")] });
  const ticks = [];
  let reloaded = 0;
  const el = document.createElement("a");
  el.dataset.ticketId = "t1";
  el.dataset.signature = documentSignature([{ id: "d1", version: "2026-10-05T12:00:00Z" }]);
  el.dataset.count = "1";
  const { instance } = mount(
    documentWatch({
      gateway: memory.gateway,
      reload: () => reloaded++,
      setInterval: (fn, ms) => ticks.push({ fn, ms }),
      clearInterval: () => {},
      isVisible: () => visible,
    }),
    { el },
  );
  instance.init();
  return { memory, instance, ticks, reloaded: () => reloaded };
}

test("starts from what the page was rendered with, asking every POLL_MS", () => {
  const { instance, ticks } = setup();
  assert.deepEqual([instance.count, instance.changed], [1, false]);
  assert.equal(ticks[0].ms, POLL_MS);
});

test("a document an agent writes raises the count and marks the page changed", async () => {
  const { instance, memory } = setup();
  await instance.check();
  assert.equal(instance.changed, false, "nothing new yet");
  memory.writeDocument(doc("d2"));
  await instance.check();
  assert.deepEqual([instance.count, instance.changed], [2, true]);
});

test("a rewritten document counts as a change too", async () => {
  const { instance, memory } = setup();
  memory.writeDocument(doc("d1", "2026-10-05T13:00:00Z"));
  await instance.check();
  assert.deepEqual([instance.count, instance.changed], [1, true]);
});

test("a hidden tab does not ask", async () => {
  const { instance, memory, ticks } = setup({ visible: false });
  memory.writeDocument(doc("d2"));
  ticks[0].fn();
  await Promise.resolve();
  assert.equal(instance.count, 1);
});

test("reload reloads the page", () => {
  const { instance, reloaded } = setup();
  instance.reload();
  assert.equal(reloaded(), 1);
});

test("the person's own move is not a change: the watch takes the moved version as loaded", async () => {
  const { instance, memory } = setup();
  const moved = await memory.gateway.setDocumentScope("d1", "project");
  instance.moved({ detail: moved });
  await instance.check();
  assert.deepEqual([instance.count, instance.changed], [1, false]);
});

test("a document an agent writes after the person's move still marks the page changed", async () => {
  const { instance, memory } = setup();
  instance.moved({ detail: await memory.gateway.setDocumentScope("d1", "project") });
  memory.writeDocument(doc("d2"));
  await instance.check();
  assert.deepEqual([instance.count, instance.changed], [2, true]);
});
