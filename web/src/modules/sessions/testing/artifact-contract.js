// The ArtifactGateway contract (../domain/ports.js), run by every
// implementation. A fake that passes it behaves like the real gateway as far
// as the Design tab can tell.

import { Codes } from "../../../shared/domain/errors.js";
import { FeedStatus } from "../../../shared/domain/feed.js";
import { flush } from "../../../shared/testing/doubles.js";
import { assert, test } from "../../../shared/testing/test.js";
import { A0, makeArtifact, toArtifactDTO } from "./artifact-fixtures.js";

/**
 * @typedef {object} Subject
 * @property {import("../domain/ports.js").ArtifactGateway} gateway
 * @property {(input: object) => import("../domain/artifact.js").Artifact} publish  as a session's publish_artifact tool would
 * @property {(sessionId: string) => void} end  the session ends
 */

/**
 * @param {string} name
 * @param {(world: { artifacts: object[], tickets?: { id: string, projectId: string }[] }) => Subject} makeSubject
 */
export function artifactGatewayContract(name, makeSubject) {
  const contract = (title, fn) => test(`${name} · ${title}`, fn);
  const later = (s) => new Date(A0.getTime() + s * 1000);

  contract("list returns the session's artifacts newest first; an unknown session lists nothing", async () => {
    const a = makeArtifact({ id: "a", sessionId: "s1", updatedAt: A0 });
    const b = makeArtifact({ id: "b", sessionId: "s1", path: "b.html", updatedAt: later(5) });
    const c = makeArtifact({ id: "c", sessionId: "s2", updatedAt: later(9) });
    const { gateway } = makeSubject({ artifacts: [a, b, c] });
    const list = await gateway.list("s1");
    assert.deepEqual(list.map((x) => x.id), ["b", "a"]);
    assert.ok(list[0].updatedAt instanceof Date);
    assert.equal(list[0].src, "/api/artifacts/b/view/");
    assert.deepEqual(await gateway.list("nobody"), []);
  });

  contract("a url artifact loads from its url; a file from the view route", async () => {
    const page = makeArtifact({ id: "p", kind: "page", path: "index.html" });
    const url = makeArtifact({ id: "u", kind: "url", path: "", url: "http://localhost:5173/", mime: "", sizeBytes: 0 });
    const { gateway } = makeSubject({ artifacts: [page, url] });
    const by = Object.fromEntries((await gateway.list("s1")).map((a) => [a.id, a]));
    assert.equal(by.p.src, "/api/artifacts/p/view/");
    assert.equal(by.u.src, "http://localhost:5173/");
    assert.equal(by.u.path, "");
  });

  contract("follow reports live, delivers the session's publishes only, and stops when closed", async () => {
    const subject = makeSubject({ artifacts: [] });
    const events = [];
    const statuses = [];
    const close = subject.gateway.follow("s1", (e) => events.push(e), (s) => statuses.push(s));
    await flush();
    assert.ok(statuses.includes(FeedStatus.LIVE), "reports live once connected");

    subject.publish({ sessionId: "s1", kind: "page", title: "Hero", note: "first", path: "hero.html", mime: "text/html", sizeBytes: 10 });
    subject.publish({ sessionId: "s2", kind: "image", title: "Other", note: "", path: "x.png", mime: "image/png", sizeBytes: 1 });
    await flush();
    assert.deepEqual(events.map((e) => [e.kind, e.artifact.title]), [["published", "Hero"]]);
    assert.equal(events[0].artifact.revision, 1);

    close();
    subject.publish({ sessionId: "s1", kind: "page", title: "Late", note: "", path: "late.html", mime: "text/html", sizeBytes: 1 });
    await flush();
    assert.equal(events.length, 1, "nothing after close");
  });

  contract("re-publishing the same path keeps the id and bumps the revision, on the list and on the stream", async () => {
    const subject = makeSubject({ artifacts: [] });
    const events = [];
    const close = subject.gateway.follow("s1", (e) => events.push(e), () => {});
    const first = subject.publish({ sessionId: "s1", kind: "page", title: "Hero", note: "first", path: "hero.html", mime: "text/html", sizeBytes: 10 });
    const second = subject.publish({ sessionId: "s1", kind: "page", title: "Hero", note: "tighter spacing", path: "hero.html", mime: "text/html", sizeBytes: 12 });
    await flush();
    assert.equal(second.id, first.id);
    assert.equal(second.revision, 2);
    assert.deepEqual(events.map((e) => e.artifact.revision), [1, 2]);
    assert.equal(events[1].artifact.note, "tighter spacing");
    const list = await subject.gateway.list("s1");
    assert.deepEqual(list.map((a) => `${a.id}@${a.revision}`), [`${first.id}@2`]);
    close();
  });

  contract("setScope moves an artifact: a new updatedAt, the same id and revision; the same scope again changes nothing", async () => {
    const { gateway } = makeSubject({ artifacts: [makeArtifact({ id: "a", revision: 3, updatedAt: A0 })] });
    const moved = await gateway.setScope("a", "project");
    assert.deepEqual([moved.id, moved.revision, moved.scope], ["a", 3, "project"]);
    assert.ok(moved.updatedAt.getTime() > A0.getTime(), "a move is a change");
    const again = await gateway.setScope("a", "project");
    assert.equal(again.updatedAt.getTime(), moved.updatedAt.getTime(), "the same scope again changes nothing");
    assert.equal((await gateway.setScope("a", "task")).scope, "task");
    assert.equal((await gateway.list("s1"))[0].scope, "task", "the session keeps it in both scopes");
  });

  contract("setScope refuses an unknown artifact, an unknown scope, and a url going to the project", async () => {
    const url = makeArtifact({ id: "u", kind: "url", path: "", url: "http://localhost:5173/", mime: "", sizeBytes: 0 });
    const { gateway } = makeSubject({ artifacts: [makeArtifact({ id: "a" }), url] });
    await assert.rejects(gateway.setScope("ghost", "project"), Codes.ARTIFACT_NOT_FOUND);
    await assert.rejects(gateway.setScope("a", "global"), Codes.INVALID_INPUT);
    await assert.rejects(gateway.setScope("u", "project"), Codes.ARTIFACT_NOT_PROMOTABLE);
  });

  contract("listProject lists the project's project-scoped artifacts only, newest first", async () => {
    const { gateway } = makeSubject({
      artifacts: [
        makeArtifact({ id: "old", scope: "project", updatedAt: A0 }),
        makeArtifact({ id: "new", path: "n.html", scope: "project", updatedAt: later(5) }),
        makeArtifact({ id: "task", path: "t.html", scope: "task", updatedAt: later(9) }),
        makeArtifact({ id: "other", path: "o.html", projectId: "p2", scope: "project", updatedAt: later(9) }),
      ],
    });
    assert.deepEqual((await gateway.listProject("p1")).map((a) => a.id), ["new", "old"]);
    assert.deepEqual(await gateway.listProject("nobody"), []);
  });

  contract("a move is delivered on the producing session's stream, with its new scope", async () => {
    const subject = makeSubject({ artifacts: [makeArtifact({ id: "a" })] });
    const events = [];
    const close = subject.gateway.follow("s1", (e) => events.push(e), () => {});
    await flush();
    await subject.gateway.setScope("a", "project");
    await flush();
    assert.deepEqual(events.map((e) => [e.kind, e.artifact.id, e.artifact.scope, e.artifact.revision]), [["published", "a", "project", 1]]);
    close();
  });

  contract("remove deletes an artifact in either scope; an unknown one is ARTIFACT_NOT_FOUND", async () => {
    const { gateway } = makeSubject({ artifacts: [makeArtifact({ id: "a", scope: "project" })] });
    await gateway.remove("a");
    assert.deepEqual(await gateway.listProject("p1"), []);
    await assert.rejects(gateway.remove("a"), Codes.ARTIFACT_NOT_FOUND);
  });

  // ── attachments ───────────────────────────────────────────────────────
  const TICKETS = [
    { id: "t1", projectId: "p1" },
    { id: "t2", projectId: "p1" },
    { id: "t3", projectId: "p1" },
    { id: "x1", projectId: "p2" },
  ];
  const asset = (overrides = {}) => makeArtifact({ id: "logo", ticketId: "t1", scope: "project", path: "logo.svg", ...overrides });

  contract("listTask is what the task produced, in either scope, and the project assets attached to it, newest first", async () => {
    const { gateway } = makeSubject({
      tickets: TICKETS,
      artifacts: [
        makeArtifact({ id: "draft", ticketId: "t2", sessionId: "s2", path: "d.html", updatedAt: A0 }),
        makeArtifact({ id: "kept", ticketId: "t2", sessionId: "s2", path: "k.html", scope: "project", updatedAt: later(2) }),
        asset({ attachedTicketIds: ["t2"], updatedAt: later(1) }),
        makeArtifact({ id: "elsewhere", ticketId: "t3", sessionId: "s3", path: "e.html", scope: "project", updatedAt: later(9) }),
      ],
    });
    assert.deepEqual((await gateway.listTask("t2")).map((a) => a.id), ["kept", "logo", "draft"]);
    assert.deepEqual((await gateway.listTask("t1")).map((a) => a.id), ["logo"]);
    assert.deepEqual(await gateway.listTask("nobody"), []);
  });

  contract("attach links a project asset to a task without touching revision or updatedAt; again, or on its own task, changes nothing", async () => {
    const { gateway } = makeSubject({ tickets: TICKETS, artifacts: [asset({ revision: 2, updatedAt: A0 })] });
    const attached = await gateway.attach("logo", "t2");
    assert.deepEqual([...attached.attachedTicketIds], ["t2"]);
    assert.deepEqual([attached.revision, attached.updatedAt.getTime()], [2, A0.getTime()]);
    assert.deepEqual([...(await gateway.attach("logo", "t2")).attachedTicketIds], ["t2"], "attaching again");
    assert.deepEqual([...(await gateway.attach("logo", "t1")).attachedTicketIds], ["t2"], "attaching to the producing task");
    assert.deepEqual([...(await gateway.attach("logo", "t3")).attachedTicketIds], ["t2", "t3"]);
    assert.deepEqual((await gateway.listTask("t3")).map((a) => a.id), ["logo"]);
    assert.deepEqual([...(await gateway.listProject("p1"))[0].attachedTicketIds], ["t2", "t3"], "every listing carries the attachments");
  });

  contract("attach refuses a missing id, an unknown artifact or task, a task-scope artifact and a task of another project", async () => {
    const { gateway } = makeSubject({ tickets: TICKETS, artifacts: [asset(), makeArtifact({ id: "draft", path: "d.html" })] });
    await assert.rejects(gateway.attach("", "t2"), Codes.INVALID_INPUT);
    await assert.rejects(gateway.attach("logo", ""), Codes.INVALID_INPUT);
    await assert.rejects(gateway.attach("ghost", "t2"), Codes.ARTIFACT_NOT_FOUND);
    await assert.rejects(gateway.attach("logo", "nobody"), Codes.TICKET_NOT_FOUND);
    await assert.rejects(gateway.attach("draft", "t2"), Codes.ARTIFACT_NOT_IN_PROJECT);
    await assert.rejects(gateway.attach("logo", "x1"), Codes.ARTIFACT_PROJECT_MISMATCH);
    assert.deepEqual([...(await gateway.listProject("p1"))[0].attachedTicketIds], [], "nothing was attached");
  });

  contract("detach unlinks a task; one it is not on changes nothing; its own task is ARTIFACT_PRODUCER_TASK", async () => {
    const { gateway } = makeSubject({ tickets: TICKETS, artifacts: [asset({ attachedTicketIds: ["t2", "t3"] })] });
    assert.deepEqual([...(await gateway.detach("logo", "t2")).attachedTicketIds], ["t3"]);
    assert.deepEqual([...(await gateway.detach("logo", "t2")).attachedTicketIds], ["t3"], "detaching again");
    assert.deepEqual(await gateway.listTask("t2"), []);
    await assert.rejects(gateway.detach("logo", "t1"), Codes.ARTIFACT_PRODUCER_TASK);
    await assert.rejects(gateway.detach("ghost", "t3"), Codes.ARTIFACT_NOT_FOUND);
    await assert.rejects(gateway.detach("logo", ""), Codes.INVALID_INPUT);
  });

  contract("moving back to the task detaches it everywhere; deleting takes its attachments with it", async () => {
    const { gateway } = makeSubject({
      tickets: TICKETS,
      artifacts: [asset({ attachedTicketIds: ["t2"] }), makeArtifact({ id: "icon", ticketId: "t1", path: "icon.svg", scope: "project", attachedTicketIds: ["t3"] })],
    });
    const back = await gateway.setScope("logo", "task");
    assert.deepEqual([back.scope, [...back.attachedTicketIds]], ["task", []]);
    assert.deepEqual(await gateway.listTask("t2"), []);
    const again = await gateway.setScope("logo", "project");
    assert.deepEqual([...again.attachedTicketIds], [], "moving to the project attaches nothing");
    await gateway.remove("icon");
    assert.deepEqual(await gateway.listTask("t3"), []);
  });

  contract("followProject delivers the project's artifact changes only, reports live, and stops when closed", async () => {
    const subject = makeSubject({
      tickets: TICKETS,
      artifacts: [asset(), makeArtifact({ id: "draft", path: "d.html" }), makeArtifact({ id: "far", projectId: "p2", ticketId: "x1", sessionId: "sx", path: "f.html", scope: "project" })],
    });
    const { gateway } = subject;
    const changes = [];
    const statuses = [];
    const close = gateway.followProject("p1", (c) => changes.push(c), (s) => statuses.push(s));
    await flush();
    assert.ok(statuses.includes(FeedStatus.LIVE), "reports live once connected");

    await gateway.attach("logo", "t2");
    await gateway.detach("logo", "t2");
    await gateway.setScope("draft", "project");
    await gateway.setScope("far", "task");
    await gateway.remove("logo");
    await flush();
    assert.deepEqual(
      changes.map((c) => (c.kind === "deleted" ? ["deleted", c.id, c.ticketId] : ["changed", c.artifact.id, c.artifact.scope, c.artifact.attachedTicketIds.join()])),
      [
        ["changed", "logo", "project", "t2"],
        ["changed", "logo", "project", ""],
        ["changed", "draft", "project", ""],
        ["deleted", "logo", "t1"],
      ],
      "another project's move is not delivered; the feed's session and ticket keys are not either",
    );

    close();
    await gateway.attach("draft", "t2");
    await flush();
    assert.equal(changes.length, 4, "nothing after close");
  });

  contract("a re-publish of a project asset is a change on the project feed, attachments kept", async () => {
    const subject = makeSubject({ tickets: TICKETS, artifacts: [] });
    const first = subject.publish({ sessionId: "s1", kind: "page", title: "Logo", note: "", path: "logo.html", mime: "text/html", sizeBytes: 1 });
    await subject.gateway.setScope(first.id, "project");
    await subject.gateway.attach(first.id, "t2");
    const changes = [];
    const close = subject.gateway.followProject("p1", (c) => changes.push(c), () => {});
    await flush();
    subject.publish({ sessionId: "s1", kind: "page", title: "Logo", note: "darker", path: "logo.html", mime: "text/html", sizeBytes: 2 });
    await flush();
    assert.deepEqual(changes.map((c) => [c.artifact.revision, c.artifact.note, [...c.artifact.attachedTicketIds]]), [[2, "darker", ["t2"]]]);
    close();
  });

  contract("decodeArtifacts reads seeded rows in the wire shape; garbage is BAD_RESPONSE", async () => {
    const { gateway } = makeSubject({ artifacts: [] });
    const [a] = gateway.decodeArtifacts([toArtifactDTO(asset({ attachedTicketIds: ["t2"] }))]);
    assert.deepEqual([a.id, a.scope, [...a.attachedTicketIds], a.src], ["logo", "project", ["t2"], "/api/artifacts/logo/view/"]);
    assert.throws(() => gateway.decodeArtifacts([{ id: "" }]), Codes.BAD_RESPONSE);
    assert.throws(() => gateway.decodeArtifacts("rows"), Codes.BAD_RESPONSE);
  });

  contract("the session's end is delivered as ended", async () => {
    const subject = makeSubject({ artifacts: [] });
    const events = [];
    const close = subject.gateway.follow("s1", (e) => events.push(e), () => {});
    await flush();
    subject.end("s1");
    await flush();
    assert.deepEqual(events, [{ kind: "ended" }]);
    close();
  });
}
