import { Codes, StructuredError } from "../../../../shared/domain/errors.js";
import { mount } from "../../../../shared/testing/alpine.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { memoryProjects } from "../../infrastructure/memory-gateway.js";
import { newProject } from "./newProject.js";

file("projects/presentation/newProject");

const pool = [
  { path: "/src/pool/api", name: "api", remote: "git@github.com:me/api.git" },
  { path: "/src/pool/libs/kit", name: "kit", remote: "" },
  { path: "/src/pool/web", name: "web", remote: "" },
];

function setup(disk = { "/src/pool": pool, "/src/empty": [] }) {
  const memory = memoryProjects({ disk });
  const visited = [];
  const mounted = mount(newProject({ gateway: memory.gateway, navigate: (url) => visited.push(url) }));
  return { ...mounted, memory, visited };
}

test("finds the repositories inside the directory, all ticked", async () => {
  const { instance } = setup();
  instance.rootDir = "/src/pool";
  await instance.search();
  assert.deepEqual(instance.found.map((f) => [f.relative, f.selected]), [["api", true], ["libs/kit", true], ["web", true]]);
  assert.equal(instance.submitLabel, "Create project with 3 repositories");
});

test("creates the project with the ticked repositories, then opens it", async () => {
  const { instance, memory, visited } = setup();
  instance.rootDir = "/src/pool";
  await instance.search();
  instance.found[2].selected = false; // not web
  assert.equal(instance.submitLabel, "Create project with 2 repositories");
  await instance.submit();

  const [project] = memory.projects();
  assert.deepEqual([project.name, project.rootDir], ["pool", "/src/pool"]);
  assert.deepEqual(
    memory.repositories(project.id).map((r) => [r.name, r.rootDir, r.url]),
    [["api", "/src/pool/api", "git@github.com:me/api.git"], ["kit", "/src/pool/libs/kit", "file:///src/pool/libs/kit"]],
  );
  assert.deepEqual(visited, [`/projects/${project.id}`]);
});

test("submitting before searching searches first", async () => {
  const { instance, memory } = setup();
  instance.rootDir = "/src/pool";
  await instance.submit();
  assert.equal(memory.repositories(memory.projects()[0].id).length, 3);
});

test("a directory without repositories still makes a project", async () => {
  const { instance, memory, visited } = setup();
  instance.rootDir = "/src/empty";
  instance.name = "Empty";
  await instance.search();
  assert.equal(instance.foundNothing, true);
  assert.equal(instance.submitLabel, "Create project");
  await instance.submit();
  assert.equal(memory.projects()[0].name, "Empty");
  assert.equal(visited.length, 1);
});

test("a wrong directory is refused before anything is created", async () => {
  const { instance, memory } = setup();
  instance.rootDir = "src/pool";
  assert.ok(instance.pathProblem, "relative paths are refused at once");
  assert.equal(instance.cannotSubmit, true);

  instance.rootDir = "/src/nowhere";
  await instance.submit();
  assert.equal(instance.error.code, Codes.INVALID_ROOT);
  assert.equal(memory.projects().length, 0);
});

test("a failed repository is retried without creating the project again or re-adding the others", async () => {
  const { instance, memory, visited } = setup();
  instance.rootDir = "/src/pool";
  await instance.search();
  const realAdd = memory.gateway.addRepository;
  let calls = 0;
  memory.gateway.addRepository = async (input) => {
    calls++;
    if (input.name === "kit") throw new StructuredError(Codes.INVALID_URL, "repository url is required", 400);
    return realAdd(input);
  };
  await instance.submit();
  assert.equal(instance.error.code, Codes.INVALID_URL);
  assert.ok(instance.createdProjectHref.endsWith("/repositories"));
  assert.deepEqual(visited, []);

  memory.gateway.addRepository = realAdd;
  await instance.submit();
  const [project] = memory.projects();
  assert.equal(memory.projects().length, 1, "one project");
  assert.deepEqual(memory.repositories(project.id).map((r) => r.name).sort(), ["api", "kit", "web"], "each once");
  assert.equal(calls, 2, "api added, kit failed; the retry used the real gateway");
  assert.equal(visited.length, 1);
});
