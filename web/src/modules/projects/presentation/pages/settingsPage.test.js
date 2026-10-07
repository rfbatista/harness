import { Codes } from "../../../../shared/domain/errors.js";
import { flush } from "../../../../shared/testing/doubles.js";
import { mount, seededElement } from "../../../../shared/testing/alpine.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { memoryProjects } from "../../infrastructure/memory-gateway.js";
import { DIRS, HOME, makeRepository, projectDTO, repositoryDTO } from "../../testing/fixtures.js";
import { settingsPage } from "./settingsPage.js";

file("projects/presentation/settingsPage");

const harness = { id: "p1", name: "harness", rootDir: "/src/harness", ignoredPaths: ["node_modules"] };

function setup({ projects = [harness, { id: "p2", name: "coding_pool", rootDir: "/src/coding_pool" }], sessions = [] } = {}) {
  const memory = memoryProjects({ projects, dirs: DIRS, home: HOME, sessions });
  const repos = [makeRepository({ projectId: "p1" })];
  const el = seededElement({ project: projectDTO(projects[0]), repositories: repos.map(repositoryDTO) });
  const visited = [];
  const mounted = mount(settingsPage({ gateway: memory.gateway, navigate: (url) => visited.push(url) }), { el });
  mounted.instance.init();
  mounted.tick();
  return { ...mounted, memory, visited };
}

test("starts from the seed, with nothing to save", () => {
  const { instance } = setup();
  assert.deepEqual([instance.name, instance.rootDir, instance.project.ignoredPaths, instance.repositories.length], ["harness", "/src/harness", ["node_modules"], 1]);
  assert.equal(instance.cannotSave, true);
});

test("saves only what changed, and shows the directory the server expanded", async () => {
  const { instance, memory } = setup();
  const sent = [];
  const update = memory.gateway.updateProject;
  memory.gateway.updateProject = (input) => (sent.push(input), update(input));
  instance.rootDir = "~/work";
  await instance.save();
  assert.deepEqual(sent, [{ projectId: "p1", rootDir: "~/work" }]);
  assert.equal(instance.rootDir, `${HOME}/work`);
  assert.equal(instance.notice, "Saved.");
  assert.equal(instance.isDirty, false);
});

test("a taken name shows under Name, a bad directory under Directory; what was typed stays", async () => {
  const { instance } = setup();
  instance.name = "Coding_Pool";
  await instance.save();
  assert.equal(instance.nameError.code, Codes.PROJECT_NAME_TAKEN);
  assert.equal(instance.name, "Coding_Pool");

  instance.name = "harness";
  instance.rootDir = "relative/dir";
  assert.ok(instance.rootProblem, "a relative path is refused before asking");
  assert.equal(instance.cannotSave, true);
  instance.rootDir = "/src/nowhere";
  await instance.save();
  assert.equal(instance.rootError.code, Codes.PROJECT_ROOT_INVALID);
  assert.equal(instance.nameError, null);

  instance.resetGeneral();
  assert.deepEqual([instance.name, instance.rootDir, instance.rootError], ["harness", "/src/harness", null]);
});

test("a change made elsewhere updates untouched fields, and only warns over unsaved edits", async () => {
  const { instance, memory } = setup();
  await memory.gateway.updateProject({ projectId: "p1", name: "harness2" });
  assert.deepEqual([instance.name, instance.notice], ["harness2", ""]);

  instance.rootDir = "/src";
  await memory.gateway.updateProject({ projectId: "p1", name: "harness3" });
  assert.equal(instance.rootDir, "/src", "the edit is kept");
  assert.ok(instance.notice.includes("harness3"));

  await memory.gateway.updateProject({ projectId: "p2", name: "other" });
  assert.equal(instance.project.name, "harness3", "another project's change is not ours");
});

test("our own save's echo does not read as a change made elsewhere", async () => {
  const { instance } = setup();
  instance.rootDir = "~/work";
  await instance.save(); // the memory gateway echoes before it answers
  assert.equal(instance.notice, "Saved.");
});

test("ignored paths are added after a check, and removed", async () => {
  const { instance } = setup();
  instance.ignoredPath = "../up";
  await instance.addIgnored();
  assert.ok(instance.ignoredError.message);
  instance.ignoredPath = " dist/ ";
  await instance.addIgnored();
  assert.deepEqual(instance.project.ignoredPaths, ["node_modules", "dist"]);
  assert.equal(instance.ignoredPath, "");
  await instance.removeIgnored("node_modules");
  assert.deepEqual(instance.project.ignoredPaths, ["dist"]);
});

test("delete waits for the exact name, then leaves for the projects list", async () => {
  const { instance, memory, visited } = setup();
  instance.askDelete();
  instance.typedName = "Harness";
  assert.equal(instance.canDelete, false);
  instance.typedName = "harness";
  await instance.deleteProject();
  assert.deepEqual(visited, ["/projects"]);
  assert.equal(memory.projects().some((p) => p.id === "p1"), false);
});

test("a refused delete names the running sessions in its own words, linking those with a task", async () => {
  const { instance, memory, visited } = setup({
    sessions: [
      { id: "s-12345678-a", projectId: "p1", ticketId: "t1", agent: "go-developer", status: "running", lastActivityAt: null },
      { id: "s-87654321-b", projectId: "p1", ticketId: "", agent: "", status: "paused", lastActivityAt: null },
    ],
  });
  instance.askDelete();
  instance.typedName = "harness";
  await instance.deleteProject();
  assert.deepEqual(visited, []);
  assert.equal(instance.refusal.message, "harness still has 2 running sessions, so it was not deleted.");
  assert.equal(instance.refusal.code, Codes.PROJECT_HAS_RUNNING_SESSIONS);
  assert.deepEqual(
    instance.refusal.sessions.map((s) => [s.label, s.href, s.detail]),
    [
      ["go-developer", "/projects/p1/tasks/t1", "· session s-123456"],
      ["a session", "", "· session s-876543 · no task"],
    ],
  );

  memory.endSession("s-12345678-a");
  memory.endSession("s-87654321-b");
  await instance.deleteProject();
  assert.deepEqual(visited, ["/projects"]);
});

test("deleted elsewhere: the page says so and stops writing", async () => {
  const { instance, memory } = setup();
  await memory.gateway.deleteProject("p1");
  await flush();
  assert.equal(instance.deletedElsewhere, true);
  instance.name = "x";
  assert.equal(instance.cannotSave, true);
});
