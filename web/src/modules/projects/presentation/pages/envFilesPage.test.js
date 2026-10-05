import { Codes } from "../../../../shared/domain/errors.js";
import { mount, seededElement } from "../../../../shared/testing/alpine.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { memoryProjects } from "../../infrastructure/memory-gateway.js";
import { makeProject, makeRepository } from "../../testing/fixtures.js";
import { envFilesPage } from "./envFilesPage.js";

file("projects/presentation/envFilesPage");

async function setup({ files = [], checkout = { ".env": "FROM_DISK=1\n", "apps/api/.env": "API=1\n" } } = {}) {
  const memory = memoryProjects({ projects: [makeProject()], repositories: [makeRepository()], checkouts: { r1: checkout } });
  for (const f of files) await memory.gateway.saveEnvFile("r1", f.path, f.content);
  const el = seededElement({
    repository_id: "r1",
    env_files: memory.envFiles("r1").map((f) => ({ path: f.path, content: f.content, updated_at: f.updatedAt.toISOString() })),
  });
  const mounted = mount(envFilesPage({ gateway: memory.gateway }), { el });
  mounted.instance.init();
  mounted.tick();
  return { ...mounted, memory };
}

test("starts by offering to bring in .env", async () => {
  const { instance } = await setup();
  assert.equal(instance.isEmpty, true);
  assert.equal(instance.newPath, ".env");
  await instance.importFile();
  assert.deepEqual(instance.files.map((f) => [f.path, f.content]), [[".env", "FROM_DISK=1\n"]]);
  assert.equal(instance.newPath, "");
});

test("edits are saved only when changed, and can be reverted", async () => {
  const { instance, memory } = await setup({ files: [{ path: ".env", content: "A=1" }] });
  assert.equal(instance.newPath, "", ".env already exists: nothing proposed");
  assert.equal(instance.cannotSave(".env"), true, "unchanged");
  instance.files[0].draft = "A=2";
  assert.equal(instance.isDirty(".env"), true);
  instance.revert(".env");
  assert.equal(instance.isDirty(".env"), false);
  instance.files[0].draft = "A=3";
  await instance.save(".env");
  assert.equal(memory.envFiles("r1")[0].content, "A=3");
  assert.equal(instance.isDirty(".env"), false);
  assert.ok(instance.notice.includes("New sessions get it"));
});

test("adds an empty file, refuses bad or duplicate paths", async () => {
  const { instance, memory } = await setup({ files: [{ path: ".env", content: "A=1" }] });
  instance.newPath = "../secrets";
  assert.ok(instance.newPathProblem);
  instance.newPath = "./.env";
  assert.ok(instance.newPathProblem.includes("already"));
  instance.newPath = "apps/web/.env";
  await instance.addEmpty();
  assert.deepEqual(instance.files.map((f) => f.path), [".env", "apps/web/.env"]);
  assert.equal(memory.envFiles("r1").length, 2);
});

test("an import of a file the checkout lacks shows the coded error", async () => {
  const { instance } = await setup({ checkout: {} });
  await instance.importFile();
  assert.equal(instance.error.code, Codes.ENV_FILE_NOT_FOUND);
  assert.equal(instance.isEmpty, true);
});

test("removes after confirming", async () => {
  const { instance, memory } = await setup({ files: [{ path: ".env", content: "A=1" }, { path: "b/.env", content: "B=1" }] });
  instance.askRemove("b/.env");
  assert.deepEqual(instance.files.map((f) => f.confirming), [false, true]);
  instance.cancelRemove();
  assert.equal(instance.files[1].confirming, false);
  await instance.remove("b/.env");
  assert.deepEqual(instance.files.map((f) => f.path), [".env"]);
  assert.equal(memory.envFiles("r1").length, 1);
});
