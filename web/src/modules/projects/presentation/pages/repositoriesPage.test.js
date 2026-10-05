import { Codes } from "../../../../shared/domain/errors.js";
import { mount, seededElement } from "../../../../shared/testing/alpine.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { memoryProjects } from "../../infrastructure/memory-gateway.js";
import { makeProject, makeRepository, repositoryDTO } from "../../testing/fixtures.js";
import { repositoriesPage } from "./repositoriesPage.js";

file("projects/presentation/repositoriesPage");

function setup(repos = [makeRepository()]) {
  const memory = memoryProjects({ projects: [makeProject()], repositories: repos });
  const el = seededElement({ project_id: "p1", repositories: repos.map(repositoryDTO) });
  const mounted = mount(repositoriesPage({ gateway: memory.gateway }), { el });
  mounted.instance.init();
  mounted.tick();
  return { ...mounted, memory };
}

test("lists the seeded repositories", () => {
  const { instance } = setup([makeRepository(), makeRepository({ id: "r2", name: "", rootDir: "/src/kit", url: "file:///src/kit" })]);
  assert.deepEqual(
    instance.rows.map((r) => [r.name, r.remote]),
    [["harness", "git@github.com:me/harness.git"], ["kit", "local only"]],
  );
});

test("adds a repository and clears the form", async () => {
  const { instance, memory } = setup([]);
  assert.equal(instance.isEmpty, true);
  instance.rootDir = "/src/coding_pool/harness";
  await instance.add();
  assert.deepEqual(instance.rows.map((r) => r.name), ["harness"]);
  assert.equal(memory.repositories("p1")[0].url, "file:///src/coding_pool/harness");
  assert.equal(instance.rootDir, "");
});

test("removes after confirming; a failure shows the coded error", async () => {
  const { instance, memory } = setup();
  instance.askRemove("r1");
  assert.equal(instance.rows[0].confirming, true);
  instance.cancelRemove();
  assert.equal(instance.rows[0].confirming, false);

  await memory.gateway.removeRepository("r1"); // removed elsewhere meanwhile
  await instance.remove("r1");
  assert.equal(instance.error.code, Codes.REPOSITORY_NOT_FOUND);
  assert.equal(instance.repositories.length, 1, "it stays until the removal succeeds");

  const fresh = setup();
  await fresh.instance.remove("r1");
  assert.equal(fresh.instance.isEmpty, true);
});

test("suggests the checkouts in the project's directory that are not added yet", async () => {
  const disk = {
    "/src/coding_pool": [
      { path: "/src/coding_pool/harness", name: "harness", remote: "" },
      { path: "/src/coding_pool/kit", name: "kit", remote: "https://example.com/kit.git" },
    ],
  };
  const memory = memoryProjects({ projects: [makeProject({ rootDir: "/src/coding_pool" })], repositories: [makeRepository()], disk });
  const el = seededElement({ project_id: "p1", project_root: "/src/coding_pool", repositories: [repositoryDTO(makeRepository())] });
  const { instance, tick } = mount(repositoriesPage({ gateway: memory.gateway }), { el });
  instance.init();
  tick();
  await instance.searchProject();

  assert.deepEqual(instance.suggestions.map((s) => [s.relative, s.remoteLabel]), [["kit", "https://example.com/kit.git"]]);
  await instance.addFound("/src/coding_pool/kit");
  assert.deepEqual(instance.rows.map((r) => r.name), ["harness", "kit"]);
  assert.equal(instance.hasSuggestions, false, "added: no longer suggested");
});

test("a project directory that cannot be read just means no suggestions", async () => {
  const { instance } = setup();
  instance.projectRoot = "/gone";
  await instance.searchProject();
  assert.equal(instance.hasSuggestions, false);
  assert.equal(instance.error, null);
});
