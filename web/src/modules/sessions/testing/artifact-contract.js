// The ArtifactGateway contract (../domain/ports.js), run by every
// implementation. A fake that passes it behaves like the real gateway as far
// as the Design tab can tell.

import { Codes } from "../../../shared/domain/errors.js";
import { FeedStatus } from "../../../shared/domain/feed.js";
import { flush } from "../../../shared/testing/doubles.js";
import { assert, test } from "../../../shared/testing/test.js";
import { A0, makeArtifact } from "./artifact-fixtures.js";

/**
 * @typedef {object} Subject
 * @property {import("../domain/ports.js").ArtifactGateway} gateway
 * @property {(input: object) => import("../domain/artifact.js").Artifact} publish  as a session's publish_artifact tool would
 * @property {(sessionId: string) => void} end  the session ends
 */

/** @param {string} name @param {(world: { artifacts: object[] }) => Subject} makeSubject */
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
