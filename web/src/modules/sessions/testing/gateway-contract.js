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

  contract("list returns the project's sessions", async () => {
    const a = makeSession({ id: "a", projectId: "p1" });
    const b = makeSession({ id: "b", projectId: "p2" });
    const { gateway } = makeSubject({ projects: ["p1", "p2"], sessions: [a, b] });
    const list = await gateway.list("p1");
    assert.deepEqual(list.map((s) => s.id), ["a"]);
    assert.ok(list[0].updatedAt instanceof Date, "updatedAt is a Date");
  });

  contract("list of an unknown project rejects PROJECT_NOT_FOUND", async () => {
    const { gateway } = makeSubject({ projects: [], sessions: [] });
    await assert.rejects(gateway.list("nope"), Codes.PROJECT_NOT_FOUND);
  });

  contract("send to an unknown session rejects SESSION_NOT_FOUND", async () => {
    const { gateway } = makeSubject({ projects: ["p1"], sessions: [] });
    await assert.rejects(gateway.send("ghost", "hi"), Codes.SESSION_NOT_FOUND);
  });

  contract("send to a finished session rejects SESSION_NOT_RUNNING", async () => {
    const done = makeSession({ id: "d", projectId: "p1", status: "done" });
    const { gateway } = makeSubject({ projects: ["p1"], sessions: [done] });
    await assert.rejects(gateway.send("d", "hi"), Codes.SESSION_NOT_RUNNING);
  });

  contract("stop of an unknown session rejects SESSION_NOT_FOUND", async () => {
    const { gateway } = makeSubject({ projects: ["p1"], sessions: [] });
    await assert.rejects(gateway.stop("ghost"), Codes.SESSION_NOT_FOUND);
  });

  contract("decodeSeed reads the API's JSON shape and rejects garbage", () => {
    const { gateway } = makeSubject({ projects: ["p1"], sessions: [] });
    const seed = gateway.decodeSeed({
      project_id: "p1",
      sessions: [{ id: "s", project_id: "p1", task: "t", status: "idle", updated_at: "2026-10-02T12:00:00Z" }],
    });
    assert.equal(seed.projectId, "p1");
    assert.equal(seed.sessions[0].status, "idle");
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
