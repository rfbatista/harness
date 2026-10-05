import { Codes } from "../../../../shared/domain/errors.js";
import { mount } from "../../../../shared/testing/alpine.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { memoryGateway } from "../../infrastructure/memory-gateway.js";
import { newSession } from "./newSession.js";

file("sessions/presentation/newSession");

function setup({ defaultRepository = "r1", repositories = { r1: "p1" } } = {}) {
  const memory = memoryGateway({ projects: ["p1"], repositories });
  const form = document.createElement("form");
  if (defaultRepository !== null) form.dataset.defaultRepository = defaultRepository;
  const mounted = mount(() => newSession({ gateway: memory.gateway })("p1", "t1"), { el: form });
  mounted.instance.init();
  return { ...mounted, memory };
}

test("starts an interactive session on the page's task and reports it", async () => {
  const { instance, dispatched, memory } = setup();
  assert.equal(instance.repositoryId, "r1", "the default repository is preselected");
  assert.equal(instance.cannotSubmit, false, "the first message is optional");
  assert.equal(instance.autoAccept, "off", "approvals are answered in the terminal");
  instance.prompt = "  write the plan  ";
  instance.agentId = "a-reviewer";
  await instance.submit();

  const [event] = dispatched;
  assert.equal(event.name, "session-created");
  assert.deepEqual(
    [event.detail.session.ticketId, event.detail.session.task, event.detail.session.agentId, event.detail.session.runsOn],
    ["t1", "write the plan", "a-reviewer", "server"],
  );
  assert.equal((await memory.gateway.list({ projectId: "p1", ticketId: "t1" })).length, 1);
});

test("a refused start shows the coded error and keeps the draft", async () => {
  const { instance, dispatched } = setup({ repositories: { r1: "another-project" } });
  instance.prompt = "go";
  await instance.submit();
  assert.equal(instance.error.code, Codes.CROSS_PROJECT_ACCESS);
  assert.equal(instance.prompt, "go");
  assert.equal(dispatched.length, 0);
  assert.equal(instance.submitting, false);
});

test("without a repository it cannot submit", () => {
  const { instance } = setup({ defaultRepository: null });
  assert.equal(instance.cannotSubmit, true);
});

test("cancel reports back to the page", () => {
  const { instance, dispatched } = setup();
  instance.cancel();
  assert.deepEqual(dispatched, [{ name: "new-session-cancelled", detail: undefined }]);
});
