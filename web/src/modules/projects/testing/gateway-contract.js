// The ProjectGateway contract (../domain/ports.js), run by every
// implementation.

import { Codes } from "../../../shared/domain/errors.js";
import { FeedStatus } from "../../../shared/domain/feed.js";
import { flush } from "../../../shared/testing/doubles.js";
import { assert, test } from "../../../shared/testing/test.js";
import { DIRS, HOME } from "./fixtures.js";

/**
 * @param {string} name
 * @param {(world: import("../infrastructure/memory-gateway.js").World) => {
 *   gateway: import("../domain/ports.js").ProjectGateway,
 *   repositories: (projectId: string) => object[],
 *   endSession: (sessionId: string) => void,
 * }} makeSubject
 */
export function projectGatewayContract(name, makeSubject) {
  const contract = (title, fn) => test(`${name} · ${title}`, fn);
  // The server's machine: its user's home and the directories that exist on it.
  const make = (world) => makeSubject({ home: HOME, dirs: DIRS, ...world });
  const p1 = { id: "p1", name: "coding_pool", rootDir: "/src" };

  contract("createProject returns the new project", async () => {
    const { gateway } = make({ projects: [], repositories: [] });
    const project = await gateway.createProject({ name: " harness ", rootDir: "/src/harness" });
    assert.ok(project.id, "it has an id");
    assert.deepEqual([project.name, project.rootDir, project.ignoredPaths], ["harness", "/src/harness", []]);
  });

  contract("createProject needs a name, a free one, and an existing absolute directory; ~ is the server's home", async () => {
    const { gateway } = make({ projects: [p1], repositories: [] });
    await assert.rejects(gateway.createProject({ name: "  ", rootDir: "/src/harness" }), Codes.INVALID_INPUT);
    await assert.rejects(gateway.createProject({ name: "CODING_POOL", rootDir: "/src/harness" }), Codes.PROJECT_NAME_TAKEN);
    await assert.rejects(gateway.createProject({ name: "x", rootDir: "src/harness" }), Codes.PROJECT_ROOT_INVALID);
    await assert.rejects(gateway.createProject({ name: "x", rootDir: "/src/nowhere" }), Codes.PROJECT_ROOT_INVALID);
    const home = await gateway.createProject({ name: "home", rootDir: "~/work/" });
    assert.equal(home.rootDir, `${HOME}/work`, "expanded and cleaned");
  });

  contract("createProject without a root directory is PROJECT_ROOT_INVALID", async () => {
    const { gateway } = make({ projects: [], repositories: [] });
    await assert.rejects(gateway.createProject({ name: "x", rootDir: "" }), Codes.PROJECT_ROOT_INVALID);
  });

  contract("addRepository adds it to the project", async () => {
    const subject = make({ projects: [p1], repositories: [] });
    const repo = await subject.gateway.addRepository({
      projectId: "p1", name: "harness", description: "", url: "file:///src/harness", rootDir: "/src/harness",
    });
    assert.deepEqual([repo.projectId, repo.name, repo.url, repo.rootDir], ["p1", "harness", "file:///src/harness", "/src/harness"]);
    assert.deepEqual(subject.repositories("p1").map((r) => r.id), [repo.id]);
  });

  contract("addRepository needs a project and a url", async () => {
    const { gateway } = make({ projects: [p1], repositories: [] });
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
    const subject = make({ projects: [p1], repositories: [r] });
    await subject.gateway.removeRepository("r1");
    assert.deepEqual(subject.repositories("p1"), []);
    await assert.rejects(subject.gateway.removeRepository("r1"), Codes.REPOSITORY_NOT_FOUND);
  });

  contract("findRepositories lists the checkouts inside a directory", async () => {
    const found = [{ path: "/src/pool/api", name: "api", remote: "git@x:api.git" }, { path: "/src/pool/web", name: "web", remote: "" }];
    const { gateway } = make({ projects: [], repositories: [], disk: { "/src/pool": found } });
    const result = await gateway.findRepositories("/src/pool/");
    assert.equal(result.root, "/src/pool");
    assert.deepEqual(result.found.map((f) => [f.name, f.remote]), [["api", "git@x:api.git"], ["web", ""]]);
    await assert.rejects(gateway.findRepositories("relative"), Codes.INVALID_ROOT);
  });

  contract("env files are saved, replaced, imported from the checkout and deleted", async () => {
    const r = { id: "r1", projectId: "p1", name: "api", description: "", url: "u", rootDir: "/r" };
    const subject = make({ projects: [p1], repositories: [r], checkouts: { r1: { ".env": "FROM_DISK=1\n" } } });
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
    const { gateway } = make({ projects: [p1], repositories: [r] });
    await assert.rejects(gateway.saveEnvFile("r1", "../.env", "x"), Codes.INVALID_PATH);
    await assert.rejects(gateway.saveEnvFile("ghost", ".env", "x"), Codes.REPOSITORY_NOT_FOUND);
    await assert.rejects(gateway.importEnvFile("r1", "missing/.env"), Codes.ENV_FILE_NOT_FOUND);
  });

  contract("decodeRepositories reads the page seed and rejects garbage", () => {
    const { gateway } = make({ projects: [p1], repositories: [] });
    const seed = gateway.decodeRepositories({
      project_id: "p1",
      project_root: "/src",
      repositories: [{ id: "r1", project_id: "p1", name: "harness", url: "u", root_dir: "/r" }],
    });
    assert.deepEqual([seed.projectId, seed.projectRoot, seed.repositories[0].rootDir], ["p1", "/src", "/r"]);
    assert.throws(() => gateway.decodeRepositories({ repositories: [] }), Codes.BAD_RESPONSE);
  });

  contract("listProjectSummaries counts each project's repositories, open tasks and live sessions", async () => {
    const p2 = { id: "p2", name: "Alpha", rootDir: "/src/harness" };
    const p3 = { id: "p3", name: "zeta", rootDir: "/src/harness", ignoredPaths: ["node_modules"] };
    const { gateway } = make({
      projects: [p1, p2, p3],
      repositories: [
        { id: "r1", projectId: "p1", name: "a", description: "", url: "u", rootDir: "/r1" },
        { id: "r2", projectId: "p1", name: "b", description: "", url: "u", rootDir: "/r2" },
      ],
      tickets: [
        { id: "t1", projectId: "p1", status: "in_progress", updatedAt: new Date("2026-10-07T10:00:00Z") },
        { id: "t2", projectId: "p1", status: "done", updatedAt: new Date("2026-10-07T12:00:00Z") },
        { id: "t3", projectId: "p2", status: "todo", updatedAt: new Date("2026-10-01T09:00:00Z") },
      ],
      sessions: [
        { id: "s1", projectId: "p1", ticketId: "t1", agent: "go-developer", status: "running", lastActivityAt: new Date("2026-10-07T11:00:00Z") },
        { id: "s2", projectId: "p1", ticketId: "t1", agent: "", status: "waiting_approval", lastActivityAt: new Date("2026-10-07T09:00:00Z") },
        { id: "s3", projectId: "p1", ticketId: "", agent: "", status: "done", lastActivityAt: new Date("2026-10-07T13:00:00Z") },
      ],
    });
    const summaries = await gateway.listProjectSummaries();
    assert.deepEqual(summaries.map((s) => s.project.id), ["p2", "p1", "p3"], "by name, ignoring case");
    const [alpha, pool, zeta] = summaries;
    assert.deepEqual([pool.repositoryCount, pool.openTaskCount, pool.runningSessionCount], [2, 1, 2]);
    assert.equal(pool.lastActivityAt?.toISOString(), "2026-10-07T13:00:00.000Z", "a finished session's last activity still counts");
    assert.deepEqual([alpha.repositoryCount, alpha.openTaskCount, alpha.runningSessionCount], [0, 1, 0]);
    assert.equal(alpha.lastActivityAt?.toISOString(), "2026-10-01T09:00:00.000Z");
    assert.equal(zeta.lastActivityAt, null);
    assert.deepEqual(zeta.project.ignoredPaths, ["node_modules"]);
  });

  contract("listProjectSummaries without projects is empty", async () => {
    const { gateway } = make({ projects: [], repositories: [] });
    assert.deepEqual(await gateway.listProjectSummaries(), []);
  });

  contract("updateProject trims, keeps an empty field, and returns the expanded root", async () => {
    const { gateway } = make({ projects: [p1], repositories: [] });
    const renamed = await gateway.updateProject({ projectId: "p1", name: " pool ", rootDir: "" });
    assert.deepEqual([renamed.name, renamed.rootDir], ["pool", "/src"]);
    const moved = await gateway.updateProject({ projectId: "p1", rootDir: "~/work" });
    assert.deepEqual([moved.name, moved.rootDir], ["pool", `${HOME}/work`]);
  });

  contract("updateProject checks the name only when it changes and the root only when it changes", async () => {
    // A legacy pair: the same name twice, and a root that is gone.
    const legacy = { id: "p2", name: "Coding_Pool", rootDir: "/gone" };
    const { gateway } = make({ projects: [p1, legacy], repositories: [] });
    const moved = await gateway.updateProject({ projectId: "p2", name: "Coding_Pool", rootDir: "/src/harness" });
    assert.equal(moved.rootDir, "/src/harness", "an unchanged colliding name does not block the root");
    const recased = await gateway.updateProject({ projectId: "p1", name: "CODING_POOL" });
    assert.equal(recased.name, "CODING_POOL", "its own name in other capitals is not a change");
    await gateway.updateProject({ projectId: "p2", name: "harness" });
    await assert.rejects(gateway.updateProject({ projectId: "p1", name: "Harness" }), Codes.PROJECT_NAME_TAKEN);
    await assert.rejects(gateway.updateProject({ projectId: "p2", rootDir: "relative" }), Codes.PROJECT_ROOT_INVALID);
    await assert.rejects(gateway.updateProject({ projectId: "ghost", name: "x" }), Codes.PROJECT_NOT_FOUND);
  });

  contract("updateProject leaves a gone root alone when it is sent back unchanged", async () => {
    const { gateway } = make({ projects: [{ id: "p2", name: "old", rootDir: "/gone" }], repositories: [] });
    const p = await gateway.updateProject({ projectId: "p2", name: "renamed", rootDir: "/gone" });
    assert.deepEqual([p.name, p.rootDir], ["renamed", "/gone"]);
  });

  contract("deleteProject is refused while sessions run, naming them; allowed once they end", async () => {
    const subject = make({
      projects: [p1],
      repositories: [{ id: "r1", projectId: "p1", name: "a", description: "", url: "u", rootDir: "/r1" }],
      sessions: [
        { id: "s1", projectId: "p1", ticketId: "t1", agent: "go-developer", status: "running", lastActivityAt: null },
        { id: "s2", projectId: "p1", ticketId: "", agent: "", status: "paused", lastActivityAt: null },
        { id: "s3", projectId: "p1", ticketId: "t2", agent: "x", status: "failed", lastActivityAt: null },
      ],
    });
    const err = await assert.rejects(subject.gateway.deleteProject("p1"), Codes.PROJECT_HAS_RUNNING_SESSIONS);
    assert.deepEqual(err.details?.sessions, [
      { id: "s1", ticketId: "t1", agent: "go-developer" },
      { id: "s2", ticketId: "", agent: "" },
    ]);
    subject.endSession("s1");
    subject.endSession("s2");
    await subject.gateway.deleteProject("p1");
    assert.deepEqual(await subject.gateway.listProjectSummaries(), []);
    assert.deepEqual(subject.repositories("p1"), [], "its repositories go with it");
    await assert.rejects(subject.gateway.deleteProject("p1"), Codes.PROJECT_NOT_FOUND);
  });

  contract("ignored paths are added once, cleaned, and removed", async () => {
    const { gateway } = make({ projects: [p1], repositories: [] });
    await gateway.addIgnoredPath("p1", "node_modules");
    await gateway.addIgnoredPath("p1", "/web/vendor/");
    const p = await gateway.addIgnoredPath("p1", "node_modules");
    assert.deepEqual(p.ignoredPaths, ["node_modules", "web/vendor"]);
    assert.deepEqual((await gateway.removeIgnoredPath("p1", "node_modules")).ignoredPaths, ["web/vendor"]);
    await assert.rejects(gateway.addIgnoredPath("ghost", "x"), Codes.PROJECT_NOT_FOUND);
  });

  contract("the catalog feed tells each create, update, ignored path and delete, in order", async () => {
    const { gateway } = make({ projects: [p1], repositories: [] });
    const changes = [];
    const statuses = [];
    const stop = gateway.followCatalog((c) => changes.push(c), (s) => statuses.push(s));
    await flush();
    const created = await gateway.createProject({ name: "harness", rootDir: "/src/harness" });
    await gateway.updateProject({ projectId: created.id, name: "h2" });
    await gateway.addIgnoredPath(created.id, "dist");
    await gateway.deleteProject(created.id);
    await flush();
    assert.deepEqual(
      changes.map((c) => (c.kind === "deleted" ? ["deleted", c.id] : ["upsert", c.project.name, c.project.ignoredPaths.join()])),
      [["upsert", "harness", ""], ["upsert", "h2", ""], ["upsert", "h2", "dist"], ["deleted", created.id]],
    );
    assert.ok(statuses.includes(FeedStatus.LIVE), "it reports the stream live");
    stop();
    await gateway.createProject({ name: "after", rootDir: "/src/harness" });
    await flush();
    assert.equal(changes.length, 4, "nothing after stopping");
  });

  contract("decodeProjectsSeed and decodeSettingsSeed read the pages' seeds and reject garbage", () => {
    const { gateway } = make({ projects: [], repositories: [] });
    const list = gateway.decodeProjectsSeed({
      summaries: [{ project: { id: "p1", name: "a", root_dir: "/a" }, repository_count: 2, open_task_count: 1, running_session_count: 0, last_activity_at: null }],
    });
    assert.deepEqual([list[0].project.id, list[0].project.ignoredPaths, list[0].repositoryCount, list[0].lastActivityAt], ["p1", [], 2, null]);
    assert.throws(() => gateway.decodeProjectsSeed({ summaries: null }), Codes.BAD_RESPONSE);

    const settings = gateway.decodeSettingsSeed({
      project: { id: "p1", name: "a", root_dir: "/a", ignored_paths: ["dist"] },
      repositories: [{ id: "r1", project_id: "p1", name: "api", url: "u", root_dir: "/a/api" }],
    });
    assert.deepEqual([settings.project.ignoredPaths, settings.repositories[0].name], [["dist"], "api"]);
    assert.throws(() => gateway.decodeSettingsSeed({ repositories: [] }), Codes.BAD_RESPONSE);
  });
}
