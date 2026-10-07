// The ChannelGateway contract (../domain/ports.js), run by every
// implementation. A fake that passes it behaves like the real gateway as far
// as the task page can tell.

import { Codes } from "../../../shared/domain/errors.js";
import { FeedStatus } from "../../../shared/domain/feed.js";
import { flush } from "../../../shared/testing/doubles.js";
import { assert, test } from "../../../shared/testing/test.js";
import { makeCheck, makeMessage } from "./channel-fixtures.js";
import { T0 } from "./fixtures.js";

/**
 * @typedef {object} Subject
 * @property {import("../domain/ports.js").ChannelGateway} gateway
 * @property {(fields: object) => import("../domain/channel.js").TaskMessage} send  a session sends a message
 * @property {(id: string) => void} deliver  a stored message reaches its recipient
 */

/** @param {string} name @param {(world: { messages?: object[], checks?: object[] }) => Subject} makeSubject */
export function channelGatewayContract(name, makeSubject) {
  const contract = (title, fn) => test(`${name} · ${title}`, fn);
  const at = (m) => new Date(T0.getTime() + m * 60_000);

  contract("listMessages returns the task's messages oldest first, narrowed by session and since", async () => {
    const { gateway } = makeSubject({
      messages: [
        makeMessage({ id: "late", fromSessionId: "d2", createdAt: at(5) }),
        makeMessage({ id: "early", createdAt: at(1) }),
        makeMessage({ id: "reply", fromSessionId: "arch", toSessionId: "d1", kind: "reply", status: "", createdAt: at(3) }),
        makeMessage({ id: "other-task", taskId: "t2", createdAt: at(2) }),
      ],
    });
    const all = await gateway.listMessages({ ticketId: "t1" });
    assert.deepEqual(all.map((m) => m.id), ["early", "reply", "late"]);
    assert.ok(all[0].createdAt instanceof Date);
    assert.deepEqual((await gateway.listMessages({ ticketId: "t1", sessionId: "d1" })).map((m) => m.id), ["early", "reply"]);
    assert.deepEqual((await gateway.listMessages({ ticketId: "t1", since: at(1) })).map((m) => m.id), ["reply", "late"]);
    assert.deepEqual(await gateway.listMessages({ ticketId: "nothing" }), []);
  });

  contract("a message keeps its kind, status, verdict, links and delivery", async () => {
    const { gateway } = makeSubject({
      messages: [
        makeMessage({ id: "r", kind: "reply", status: "", verdict: "changes_requested", inReplyTo: "q", subject: "Rename it", documentIds: ["doc1"], artifactIds: ["a1"], delivered: false, deliveredAt: null }),
      ],
    });
    const [m] = await gateway.listMessages({ ticketId: "t1" });
    assert.deepEqual(
      [m.kind, m.status, m.verdict, m.inReplyTo, m.subject, m.documentIds, m.artifactIds, m.delivered, m.deliveredAt],
      ["reply", "", "changes_requested", "q", "Rename it", ["doc1"], ["a1"], false, null],
    );
  });

  contract("setStatusCheck pauses with 0 and resumes or retunes with a value", async () => {
    const { gateway } = makeSubject({ checks: [makeCheck({ delegateSessionId: "d1" })] });
    const paused = await gateway.setStatusCheck("d1", 0);
    assert.deepEqual([paused.delegateSessionId, paused.everyMinutes, paused.state, paused.nextAt], ["d1", 0, "paused", null]);
    const resumed = await gateway.setStatusCheck("d1", 30);
    assert.deepEqual([resumed.everyMinutes, resumed.state], [30, "active"]);
    assert.ok(resumed.nextAt instanceof Date);
  });

  contract("setStatusCheck refuses a session without a loop and an interval out of range", async () => {
    const { gateway } = makeSubject({ checks: [makeCheck({ delegateSessionId: "d1" })] });
    await assert.rejects(gateway.setStatusCheck("peer", 10), Codes.STATUS_CHECK_NOT_FOUND);
    await assert.rejects(gateway.setStatusCheck("d1", 1), Codes.INVALID_INPUT);
    await assert.rejects(gateway.setStatusCheck("d1", 241), Codes.INVALID_INPUT);
  });

  contract("follow reports live, delivers each message as sent and again as delivered, and stops when closed", async () => {
    const subject = makeSubject({});
    const events = [];
    const statuses = [];
    const close = subject.gateway.follow("p1", (e) => events.push(e), (s) => statuses.push(s));
    await flush();
    assert.ok(statuses.includes(FeedStatus.LIVE), "reports live once connected");

    const sent = subject.send({ taskId: "t1", fromSessionId: "arch", toSessionId: "d1", kind: "reply", body: "Go on.", delivered: false, deliveredAt: null });
    await flush();
    subject.deliver(sent.id);
    await flush();
    assert.deepEqual(events.map((e) => [e.kind, e.message.id, e.message.delivered]), [["message", sent.id, false], ["message", sent.id, true]]);

    close();
    subject.send({ taskId: "t1", fromSessionId: "d1", toSessionId: "arch", kind: "question", body: "Late?" });
    await flush();
    assert.equal(events.length, 2, "nothing after close");
  });
}
