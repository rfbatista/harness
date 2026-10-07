// The SessionGateway contract (../domain/ports.js), run by every
// implementation — the browser twin of internal/ports/portstest. A fake that
// passes it behaves like the real gateway as far as pages can tell.

import { Codes } from "../../../shared/domain/errors.js";
import { FeedStatus } from "../../../shared/domain/feed.js";
import { flush } from "../../../shared/testing/doubles.js";
import { assert, test } from "../../../shared/testing/test.js";

/**
 * @typedef {object} Subject
 * @property {import("../domain/ports.js").SessionGateway} gateway
 * @property {(session: import("../domain/session.js").Session) => void} upsert
 *           Changes the world behind the gateway, as the server would.
 */

/**
 * @param {string} name
 * @param {(world: { projects: string[], sessions: object[] }) => Subject} makeSubject
 * @param {(overrides?: object) => import("../domain/session.js").Session} makeSession
 */
export function sessionGatewayContract(name, makeSubject, makeSession) {
  const contract = (title, fn) => test(`${name} · ${title}`, fn);

  contract("list returns the project's sessions, or one task's", async () => {
    const a = makeSession({ id: "a", projectId: "p1", ticketId: "t1" });
    const a2 = makeSession({ id: "a2", projectId: "p1", ticketId: "t1" });
    const c = makeSession({ id: "c", projectId: "p1", ticketId: "t2" });
    const b = makeSession({ id: "b", projectId: "p2", ticketId: "t9" });
    const { gateway } = makeSubject({ projects: ["p1", "p2"], sessions: [a, a2, c, b] });

    const project = await gateway.list({ projectId: "p1" });
    assert.deepEqual(project.map((s) => s.id).sort(), ["a", "a2", "c"]);
    assert.ok(project[0].updatedAt instanceof Date, "updatedAt is a Date");

    const task = await gateway.list({ projectId: "p1", ticketId: "t1" });
    assert.deepEqual(task.map((s) => s.id).sort(), ["a", "a2"], "a task can have several sessions");
    assert.equal(task[0].ticketId, "t1");
  });

  contract("list of an unknown project rejects PROJECT_NOT_FOUND", async () => {
    const { gateway } = makeSubject({ projects: [], sessions: [] });
    await assert.rejects(gateway.list({ projectId: "nope" }), Codes.PROJECT_NOT_FOUND);
  });

  contract("stop of an unknown session rejects SESSION_NOT_FOUND", async () => {
    const { gateway } = makeSubject({ projects: ["p1"], sessions: [] });
    await assert.rejects(gateway.stop("ghost"), Codes.SESSION_NOT_FOUND);
  });

  contract("start runs an interactive session on the server, on the task, and puts it on the feed", async () => {
    const subject = makeSubject({ projects: ["p1"], sessions: [] });
    const changes = [];
    const close = subject.gateway.follow("p1", (c) => changes.push(c), () => {});
    const session = await subject.gateway.start({
      projectId: "p1", ticketId: "t1", repositoryId: "r1", agentId: "", prompt: "  write the plan  ", autoAccept: "off",
      size: { cols: 120, rows: 32 },
    });
    assert.ok(session.id, "the new session has an id");
    assert.equal(session.ticketId, "t1");
    assert.equal(session.task, "write the plan");
    assert.ok(session.interactive && session.runsOn === "server", "it runs in a PTY on the server");
    assert.ok(!["done", "failed", "stopped"].includes(session.status), "a new session is alive");
    await flush();
    assert.ok(changes.some((c) => c.kind === "upsert" && c.session.id === session.id), "followers see it");
    const listed = await subject.gateway.list({ projectId: "p1", ticketId: "t1" });
    assert.deepEqual(listed.map((s) => s.id), [session.id]);
    close();
  });

  contract("start without a repository is INVALID_INPUT; without a prompt it opens idle", async () => {
    const { gateway } = makeSubject({ projects: ["p1"], sessions: [] });
    await assert.rejects(gateway.start({ projectId: "p1", ticketId: "t1", repositoryId: "", prompt: "go", size: { cols: 80, rows: 24 } }), Codes.INVALID_INPUT);
    const idle = await gateway.start({ projectId: "p1", ticketId: "t1", repositoryId: "r1", prompt: "", size: { cols: 80, rows: 24 } });
    assert.equal(idle.task, "");
  });

  contract("start keeps the mode it was asked for and refuses an unknown one", async () => {
    const { gateway } = makeSubject({ projects: ["p1"], sessions: [] });
    const base = { projectId: "p1", ticketId: "t1", repositoryId: "r1", agentId: "", prompt: "", autoAccept: "off", size: { cols: 80, rows: 24 } };
    assert.equal((await gateway.start({ ...base, mode: "design" })).mode, "design");
    assert.equal((await gateway.start({ ...base, mode: "architect" })).mode, "architect");
    assert.equal((await gateway.start({ ...base })).mode, "");
    await assert.rejects(gateway.start({ ...base, mode: "painter" }), Codes.INVALID_INPUT);
  });

  contract("remove deletes the session and tells followers", async () => {
    const s = makeSession({ id: "gone", projectId: "p1" });
    const subject = makeSubject({ projects: ["p1"], sessions: [s] });
    const changes = [];
    const close = subject.gateway.follow("p1", (c) => changes.push(c), () => {});
    await subject.gateway.remove("gone");
    await flush();
    assert.deepEqual(changes.at(-1), { kind: "deleted", id: "gone" });
    assert.deepEqual(await subject.gateway.list({ projectId: "p1" }), []);
    await assert.rejects(subject.gateway.remove("gone"), Codes.SESSION_NOT_FOUND);
    close();
  });

  const ended = (overrides = {}) =>
    makeSession({ projectId: "p1", status: "stopped", interactive: true, runsOn: "server", resumable: true, resumeBlocked: "", ...overrides });
  const size = { cols: 120, rows: 32 };

  contract("resume brings an ended session back on the server, same id, and puts it on the feed", async () => {
    const subject = makeSubject({ projects: ["p1"], sessions: [ended({ id: "back", runsOn: "tui", runnerHost: "laptop" })] });
    const changes = [];
    const close = subject.gateway.follow("p1", (c) => changes.push(c), () => {});
    const session = await subject.gateway.resume("back", size);
    assert.equal(session.id, "back");
    assert.equal(session.status, "running");
    assert.equal(session.runsOn, "server", "the web resumes on the server, wherever it ran before");
    assert.equal(session.resumable, false, "a running session is not resumable");
    await flush();
    assert.ok(changes.some((c) => c.kind === "upsert" && c.session.id === "back" && c.session.status === "running"), "followers see it come back");
    close();
  });

  contract("a second resume of the same session is SESSION_ALREADY_RUNNING", async () => {
    const { gateway } = makeSubject({ projects: ["p1"], sessions: [ended({ id: "twice" })] });
    const [first, second] = await Promise.allSettled([gateway.resume("twice", size), gateway.resume("twice", size)]);
    assert.equal(first.status, "fulfilled");
    assert.equal(second.status, "rejected");
    assert.equal(second.reason.code, Codes.SESSION_ALREADY_RUNNING);
  });

  contract("resume refuses what cannot come back, with the reason's code", async () => {
    const { gateway } = makeSubject({
      projects: ["p1"],
      sessions: [
        makeSession({ id: "alive", projectId: "p1", status: "running" }),
        ended({ id: "headless", interactive: false, runsOn: "", resumable: false, resumeBlocked: "SESSION_NOT_INTERACTIVE" }),
        ended({ id: "no-tree", resumable: false, resumeBlocked: "WORKSPACE_MISSING" }),
        ended({ id: "no-chat", resumable: false, resumeBlocked: "SESSION_TRANSCRIPT_MISSING" }),
      ],
    });
    await assert.rejects(gateway.resume("alive", size), Codes.SESSION_ALREADY_RUNNING);
    await assert.rejects(gateway.resume("headless", size), Codes.SESSION_NOT_INTERACTIVE);
    await assert.rejects(gateway.resume("no-tree", size), Codes.WORKSPACE_MISSING);
    await assert.rejects(gateway.resume("no-chat", size), Codes.SESSION_TRANSCRIPT_MISSING);
    await assert.rejects(gateway.resume("ghost", size), Codes.SESSION_NOT_FOUND);
  });

  contract("list carries whether each session can be resumed", async () => {
    const { gateway } = makeSubject({
      projects: ["p1"],
      sessions: [ended({ id: "yes" }), ended({ id: "no", resumable: false, resumeBlocked: "WORKSPACE_MISSING" })],
    });
    const byId = Object.fromEntries((await gateway.list({ projectId: "p1" })).map((s) => [s.id, s]));
    assert.deepEqual([byId.yes.resumable, byId.yes.resumeBlocked], [true, ""]);
    assert.deepEqual([byId.no.resumable, byId.no.resumeBlocked], [false, "WORKSPACE_MISSING"]);
  });

  contract("listBranches lists a repository's branches, local first", async () => {
    const branches = [
      { name: "main", remote: false, isHead: true },
      { name: "feat/x", remote: false, isHead: false },
      { name: "origin/main", remote: true, isHead: false },
    ];
    const { gateway } = makeSubject({ projects: ["p1"], sessions: [], branches: { r1: branches } });
    const got = await gateway.listBranches("r1");
    assert.deepEqual(got.map((b) => [b.name, b.remote, b.isHead]), [["main", false, true], ["feat/x", false, false], ["origin/main", true, false]]);
    await assert.rejects(gateway.listBranches("ghost"), Codes.REPOSITORY_NOT_FOUND);
  });

  contract("decodeSeed reads the API's JSON shape and rejects garbage", () => {
    const { gateway } = makeSubject({ projects: ["p1"], sessions: [] });
    const seed = gateway.decodeSeed({
      project_id: "p1",
      ticket_id: "t1",
      sessions: [{ id: "s", project_id: "p1", ticket_id: "t1", task: "t", status: "idle", updated_at: "2026-10-02T12:00:00Z" }],
    });
    assert.equal(seed.projectId, "p1");
    assert.equal(seed.ticketId, "t1");
    assert.equal(seed.sessions[0].ticketId, "t1");
    assert.equal(seed.sessions[0].status, "idle");
    assert.deepEqual([seed.sessions[0].resumable, seed.sessions[0].resumeBlocked], [false, ""]);
    assert.throws(() => gateway.decodeSeed({ sessions: [] }), Codes.BAD_RESPONSE);
  });

  contract("follow reports live, delivers the project's changes, and stops when closed", async () => {
    const subject = makeSubject({ projects: ["p1", "p2"], sessions: [] });
    const changes = [];
    const statuses = [];
    const close = subject.gateway.follow("p1", (c) => changes.push(c), (s) => statuses.push(s));
    await flush();
    assert.ok(statuses.includes(FeedStatus.LIVE), "reports live once connected");

    subject.upsert(makeSession({ id: "mine", projectId: "p1" }));
    subject.upsert(makeSession({ id: "theirs", projectId: "p2" }));
    await flush();
    assert.deepEqual(changes.map((c) => c.kind === "upsert" && c.session.id), ["mine"]);

    close();
    subject.upsert(makeSession({ id: "late", projectId: "p1" }));
    await flush();
    assert.equal(changes.length, 1, "no changes after close");
  });
}
