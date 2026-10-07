import { Codes, StructuredError } from "../../../../shared/domain/errors.js";
import { mount } from "../../../../shared/testing/alpine.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { memoryTasks } from "../../infrastructure/memory-gateway.js";
import { documentScope } from "./documentScope.js";

file("tasks/presentation/documentScope");

function setup({ scope = "task", gateway, afterTaskHref } = {}) {
  const memory = memoryTasks({ projects: ["p1"], documents: [{ id: "d1", ticketId: "t1", updatedAt: "2026-10-05T12:00:00Z", scope }] });
  const el = document.createElement("div");
  el.dataset.documentId = "d1";
  el.dataset.scope = scope;
  el.dataset.title = "Plan";
  if (afterTaskHref) el.dataset.afterTaskHref = afterTaskHref;
  const visited = [];
  const { instance, dispatched } = mount(documentScope({ gateway: gateway ?? memory.gateway, navigate: (url) => visited.push(url) }), { el });
  instance.init();
  return { memory, instance, dispatched, visited };
}

test("says the scope in words and offers the move it allows, named for the document", () => {
  const task = setup().instance;
  assert.deepEqual([task.word, task.mark, task.isProject, task.label, task.ariaLabel], ["Task document", "", false, "Move to project", "Move Plan to project"]);
  const project = setup({ scope: "project" }).instance;
  assert.deepEqual([project.word, project.mark, project.isProject, project.label, project.ariaLabel], ["Project document", "project", true, "Move back to task", "Move Plan back to task"]);
});

test("a move asks the gateway and flips the word, the mark and the label in place", async () => {
  const { instance, memory } = setup();
  await instance.move();
  assert.equal(memory.documents[0].scope, "project");
  assert.deepEqual([instance.scope, instance.word, instance.label, instance.error], ["project", "Project document", "Move back to task", null]);
  await instance.move();
  assert.deepEqual([memory.documents[0].scope, instance.label], ["task", "Move to project"]);
});

test("a second click while moving sends nothing more", async () => {
  let calls = 0;
  let release;
  const gateway = { setDocumentScope: () => { calls++; return new Promise((r) => (release = r)); } };
  const { instance } = setup({ gateway });
  const first = instance.move();
  await instance.move();
  assert.equal(calls, 1);
  release({ id: "d1", version: "v2", scope: "project" });
  await first;
  assert.deepEqual([instance.moving, instance.scope], [false, "project"]);
});

test("a refused move shows the coded error in words and leaves the control usable", async () => {
  const gateway = { setDocumentScope: async () => { throw new StructuredError(Codes.DOCUMENT_NOT_FOUND, "document not found", 404); } };
  const { instance } = setup({ gateway });
  await instance.move();
  assert.deepEqual([instance.moving, instance.scope, instance.error?.code, instance.error?.message], [false, "task", "DOCUMENT_NOT_FOUND", "document not found"]);
  assert.ok(instance.error.next.length > 0, "it says what to do next");
  instance.dismissError();
  assert.equal(instance.error, null);
});

test("a move tells the page the document's new version, so its watch does not count the move as someone else's change", async () => {
  const { instance, memory, dispatched } = setup();
  await instance.move();
  assert.deepEqual(dispatched, [{ name: "document-moved", detail: { id: "d1", version: memory.documents[0].updatedAt } }]);
});

test("a refused move tells the page nothing", async () => {
  const gateway = { setDocumentScope: async () => { throw new StructuredError(Codes.DOCUMENT_NOT_FOUND, "document not found", 404); } };
  const { instance, dispatched } = setup({ gateway });
  await instance.move();
  assert.deepEqual(dispatched, []);
});

test("moved back to task from the library, the document leaves it: the page goes on to its task's page", async () => {
  const { instance, visited } = setup({ scope: "project", afterTaskHref: "/projects/p1/tasks/t1/documents/d1" });
  await instance.move();
  assert.deepEqual(visited, ["/projects/p1/tasks/t1/documents/d1"]);
});

test("on a task's page a move stays in place, both ways", async () => {
  const { instance, visited } = setup();
  await instance.move();
  await instance.move();
  assert.deepEqual([visited, instance.scope], [[], "task"]);
});

test("a refused move from the library stays on the page with the error", async () => {
  const gateway = { setDocumentScope: async () => { throw new StructuredError(Codes.DOCUMENT_NOT_FOUND, "document not found", 404); } };
  const { instance, visited } = setup({ scope: "project", gateway, afterTaskHref: "/projects/p1/tasks/t1/documents/d1" });
  await instance.move();
  assert.deepEqual([visited, instance.error?.code], [[], "DOCUMENT_NOT_FOUND"]);
});
