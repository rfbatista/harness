import { Codes } from "../../../../shared/domain/errors.js";
import { mount } from "../../../../shared/testing/alpine.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { memoryGateway } from "../../infrastructure/memory-gateway.js";
import { brainstorm, BRAINSTORM_DIALOG } from "./brainstorm.js";

file("sessions/presentation/brainstorm");

function setup({ defaultRepository = "r1", repositories = { r1: "p1" } } = {}) {
  const memory = memoryGateway({ projects: ["p1"], repositories });
  const el = document.createElement("span");
  el.dataset.projectId = "p1";
  if (defaultRepository !== null) el.dataset.defaultRepository = defaultRepository;
  const navigated = [];
  const mounted = mount(brainstorm({ gateway: memory.gateway, navigate: (url) => navigated.push(url) }), { el });
  mounted.instance.init();
  return { ...mounted, memory, navigated };
}

test("the trigger opens the dialog anchored to itself", () => {
  const { instance, dispatched } = setup();
  const button = document.createElement("button");
  instance.open({ currentTarget: button });
  assert.deepEqual(dispatched, [{ name: "dialog-open", detail: { id: BRAINSTORM_DIALOG, anchor: button } }]);
});

test("Start opens a product-mode session with no task and goes to it on the project's sessions page", async () => {
  const { instance, memory, navigated } = setup();
  assert.equal(instance.cannotStart, false, "the default repository is preselected");
  instance.agentId = "pm";
  await instance.start();
  const [session] = await memory.gateway.list({ projectId: "p1" });
  assert.deepEqual([session.ticketId, session.mode, session.agentId, session.repositoryId ?? "r1"], ["", "product", "pm", "r1"]);
  assert.deepEqual(navigated, [`/projects/p1/sessions?session=${encodeURIComponent(session.id)}`]);
  assert.equal(instance.starting, false);
});

test("without a repository it cannot start; a refused start shows the coded error and stays", async () => {
  const none = setup({ defaultRepository: null });
  assert.ok(none.instance.problem.includes("repository"));
  assert.equal(none.instance.cannotStart, true);
  await none.instance.start();
  assert.deepEqual(none.navigated, []);

  const wrong = setup({ defaultRepository: "r9", repositories: { r9: "p2" } });
  await wrong.instance.start();
  assert.equal(wrong.instance.error?.code, Codes.CROSS_PROJECT_ACCESS);
  assert.deepEqual(wrong.navigated, [], "a refusal stays in the dialog");
  assert.equal(wrong.instance.starting, false);
});
