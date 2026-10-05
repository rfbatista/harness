// The ProjectGateway contract (../domain/ports.js), run by every
// implementation.

import { Codes } from "../../../shared/domain/errors.js";
import { assert, test } from "../../../shared/testing/test.js";

/**
 * @param {string} name
 * @param {(world: { projects: object[], repositories: object[] }) => { gateway: import("../domain/ports.js").ProjectGateway, repositories: (projectId: string) => object[] }} makeSubject
 */
export function projectGatewayContract(name, makeSubject) {
  const contract = (title, fn) => test(`${name} · ${title}`, fn);
  const p1 = { id: "p1", name: "coding_pool", rootDir: "/src" };

  contract("createProject returns the new project", async () => {
    const { gateway } = makeSubject({ projects: [], repositories: [] });
    const project = await gateway.createProject({ name: "harness", rootDir: "/src/harness" });
    assert.ok(project.id, "it has an id");
    assert.deepEqual([project.name, project.rootDir], ["harness", "/src/harness"]);
  });

  contract("createProject without a root directory is INVALID_ROOT", async () => {
    const { gateway } = makeSubject({ projects: [], repositories: [] });
    await assert.rejects(gateway.createProject({ name: "x", rootDir: "" }), Codes.INVALID_ROOT);
  });

  contract("addRepository adds it to the project", async () => {
    const subject = makeSubject({ projects: [p1], repositories: [] });
    const repo = await subject.gateway.addRepository({
      projectId: "p1", name: "harness", description: "", url: "file:///src/harness", rootDir: "/src/harness",
    });
    assert.deepEqual([repo.projectId, repo.name, repo.url, repo.rootDir], ["p1", "harness", "file:///src/harness", "/src/harness"]);
    assert.deepEqual(subject.repositories("p1").map((r) => r.id), [repo.id]);
  });

  contract("addRepository needs a project and a url", async () => {
    const { gateway } = makeSubject({ projects: [p1], repositories: [] });
    await assert.rejects(
      gateway.addRepository({ projectId: "nope", name: "", description: "", url: "u", rootDir: "/r" }),
      Codes.PROJECT_NOT_FOUND,
    );
    await assert.rejects(
      gateway.addRepository({ projectId: "p1", name: "", description: "", url: "", rootDir: "/r" }),
      Codes.INVALID_URL,
    );
  });

  contract("removeRepository removes it; twice is REPOSITORY_NOT_FOUND", async () => {
    const r = { id: "r1", projectId: "p1", name: "harness", description: "", url: "u", rootDir: "/r" };
    const subject = makeSubject({ projects: [p1], repositories: [r] });
    await subject.gateway.removeRepository("r1");
    assert.deepEqual(subject.repositories("p1"), []);
    await assert.rejects(subject.gateway.removeRepository("r1"), Codes.REPOSITORY_NOT_FOUND);
  });

  contract("findRepositories lists the checkouts inside a directory", async () => {
    const found = [{ path: "/src/pool/api", name: "api", remote: "git@x:api.git" }, { path: "/src/pool/web", name: "web", remote: "" }];
    const { gateway } = makeSubject({ projects: [], repositories: [], disk: { "/src/pool": found } });
    const result = await gateway.findRepositories("/src/pool/");
    assert.equal(result.root, "/src/pool");
    assert.deepEqual(result.found.map((f) => [f.name, f.remote]), [["api", "git@x:api.git"], ["web", ""]]);
    await assert.rejects(gateway.findRepositories("relative"), Codes.INVALID_ROOT);
  });

  contract("env files are saved, replaced, imported from the checkout and deleted", async () => {
    const r = { id: "r1", projectId: "p1", name: "api", description: "", url: "u", rootDir: "/r" };
    const subject = makeSubject({ projects: [p1], repositories: [r], checkouts: { r1: { ".env": "FROM_DISK=1\n" } } });
    const { gateway } = subject;

    const saved = await gateway.saveEnvFile("r1", "apps/api/.env", "A=1");
    assert.deepEqual([saved.path, saved.content], ["apps/api/.env", "A=1"]);
    assert.ok(saved.updatedAt instanceof Date);
    assert.equal((await gateway.saveEnvFile("r1", "apps/api/.env", "A=2")).content, "A=2");

    const imported = await gateway.importEnvFile("r1", ".env");
    assert.equal(imported.content, "FROM_DISK=1\n");

    await gateway.deleteEnvFile("r1", "apps/api/.env");
    await assert.rejects(gateway.deleteEnvFile("r1", "apps/api/.env"), Codes.ENV_FILE_NOT_FOUND);
  });

  contract("env files refuse paths outside the repository, missing files and unknown repositories", async () => {
    const r = { id: "r1", projectId: "p1", name: "api", description: "", url: "u", rootDir: "/r" };
    const { gateway } = makeSubject({ projects: [p1], repositories: [r] });
    await assert.rejects(gateway.saveEnvFile("r1", "../.env", "x"), Codes.INVALID_PATH);
    await assert.rejects(gateway.saveEnvFile("ghost", ".env", "x"), Codes.REPOSITORY_NOT_FOUND);
    await assert.rejects(gateway.importEnvFile("r1", "missing/.env"), Codes.ENV_FILE_NOT_FOUND);
  });

  contract("decodeRepositories reads the page seed and rejects garbage", () => {
    const { gateway } = makeSubject({ projects: [p1], repositories: [] });
    const seed = gateway.decodeRepositories({
      project_id: "p1",
      project_root: "/src",
      repositories: [{ id: "r1", project_id: "p1", name: "harness", url: "u", root_dir: "/r" }],
    });
    assert.deepEqual([seed.projectId, seed.projectRoot, seed.repositories[0].rootDir], ["p1", "/src", "/r"]);
    assert.throws(() => gateway.decodeRepositories({ repositories: [] }), Codes.BAD_RESPONSE);
  });
}
