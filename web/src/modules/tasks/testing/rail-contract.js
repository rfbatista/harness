// The RailGateway contract (../domain/ports.js), run by every implementation.

import { assert, test } from "../../../shared/testing/test.js";
import { flush } from "../../../shared/testing/doubles.js";

const session = (id, ticketId, status) => ({ id, ticketId, status, pendingApprovals: 0 });

/**
 * @param {string} name
 * @param {(world: object) => { gateway: import("../domain/ports.js").RailGateway, emit: (change: object) => void, seed: object }} makeSubject
 */
export function railGatewayContract(name, makeSubject) {
  const contract = (title, fn) => test(`${name} · ${title}`, fn);
  const world = () => ({ projectId: "p1", sessions: [session("a", "t1", "running")] });

  contract("lists the project's sessions", async () => {
    const { gateway } = makeSubject(world());
    assert.deepEqual(await gateway.listSessions("p1"), [session("a", "t1", "running")]);
  });

  contract("follows starts, changes and deletes", async () => {
    const subject = makeSubject(world());
    const seen = [];
    const unfollow = subject.gateway.follow("p1", (c) => seen.push(c), () => {});
    await flush();
    subject.emit({ kind: "upsert", session: session("b", "t1", "idle") });
    subject.emit({ kind: "deleted", id: "a" });
    await flush();
    assert.deepEqual(seen, [{ kind: "upsert", session: session("b", "t1", "idle") }, { kind: "deleted", id: "a" }]);
    unfollow();
  });

  contract("decodes the seed the rail embeds", () => {
    const subject = makeSubject(world());
    assert.deepEqual(subject.gateway.decodeSeed(subject.seed), { projectId: "p1", sessions: [session("a", "t1", "running")] });
  });
}
