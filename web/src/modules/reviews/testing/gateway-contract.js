// The ReviewGateway contract (../domain/ports.js), run by every
// implementation. A fake that passes it behaves like the real gateway as far
// as the review band and the inbox can tell.

import { Codes } from "../../../shared/domain/errors.js";
import { FeedStatus } from "../../../shared/domain/feed.js";
import { flush } from "../../../shared/testing/doubles.js";
import { assert, test } from "../../../shared/testing/test.js";
import { makeReview, R0 } from "./fixtures.js";

/**
 * @typedef {object} Subject
 * @property {import("../domain/ports.js").ReviewGateway} gateway
 * @property {(fields: object) => import("../domain/review.js").ReviewRequest} request  the architect raises one
 * @property {(id: string) => void} withdraw  the architect withdraws one
 */

/** @param {string} name @param {(world: { reviews?: object[], delivered?: () => boolean }) => Subject} makeSubject */
export function reviewGatewayContract(name, makeSubject) {
  const contract = (title, fn) => test(`${name} · ${title}`, fn);
  const at = (m) => new Date(R0.getTime() + m * 60_000);

  contract("a task's requests in every state, newest first; a project's pending ones only", async () => {
    const { gateway } = makeSubject({
      reviews: [
        makeReview({ id: "old", createdAt: at(1), state: "approved" }),
        makeReview({ id: "new", createdAt: at(3) }),
        makeReview({ id: "other-task", taskId: "t2", createdAt: at(2) }),
        makeReview({ id: "other-project", taskId: "t9", projectId: "p2", createdAt: at(4) }),
      ],
    });
    assert.deepEqual((await gateway.listForTask("t1")).map((r) => r.id), ["new", "old"]);
    assert.deepEqual((await gateway.listPendingForProject("p1")).map((r) => r.id), ["new", "other-task"]);
    assert.deepEqual(await gateway.listForTask("nothing"), []);
    const [r] = await gateway.listForTask("t2");
    assert.ok(r.createdAt instanceof Date);
  });

  contract("a request keeps its subject, body, links and who it is about; artifacts open in their sandboxed view", async () => {
    const { gateway } = makeSubject({
      reviews: [makeReview({ documentIds: ["doc1"], artifacts: [{ id: "a 1", href: "/api/artifacts/a%201/view/" }] })],
    });
    const [r] = await gateway.listForTask("t1");
    assert.deepEqual([r.subject, r.aboutSessionId, r.documentIds, r.artifacts], ["Spec set ready for sign-off", "d1", ["doc1"], [{ id: "a 1", href: "/api/artifacts/a%201/view/" }]]);
  });

  contract("approving and requesting changes settle a pending request with the note, and say if it was delivered", async () => {
    const { gateway } = makeSubject({ reviews: [makeReview({ id: "a" }), makeReview({ id: "b" })], delivered: () => false });
    const approved = await gateway.respond({ reviewId: "a", decision: "approved", note: "" });
    assert.deepEqual([approved.review.state, approved.delivered], ["approved", false]);
    const changes = await gateway.respond({ reviewId: "b", decision: "changes_requested", note: "Rename the port." });
    assert.deepEqual([changes.review.state, changes.review.responseNote], ["changes_requested", "Rename the port."]);
    assert.ok(changes.review.respondedAt instanceof Date);
    assert.deepEqual(await gateway.listPendingForProject("p1"), []);
  });

  contract("an answer is refused for a settled request, an unknown one, or changes without a note", async () => {
    const { gateway } = makeSubject({ reviews: [makeReview({ id: "done", state: "approved" }), makeReview({ id: "open" })] });
    await assert.rejects(gateway.respond({ reviewId: "done", decision: "approved", note: "" }), Codes.REVIEW_NOT_PENDING);
    await assert.rejects(gateway.respond({ reviewId: "ghost", decision: "approved", note: "" }), Codes.REVIEW_NOT_FOUND);
    await assert.rejects(gateway.respond({ reviewId: "open", decision: "changes_requested", note: "  " }), Codes.INVALID_INPUT);
  });

  contract("follow reports live, delivers the project's requests raised and changed, and stops when closed", async () => {
    const subject = makeSubject({});
    const events = [];
    const statuses = [];
    const close = subject.gateway.follow("p1", (e) => events.push(e), (s) => statuses.push(s));
    await flush();
    assert.ok(statuses.includes(FeedStatus.LIVE), "reports live once connected");

    const raised = subject.request({ subject: "Look at the plan" });
    subject.request({ projectId: "p2", taskId: "t9", subject: "Not ours" });
    await flush();
    subject.withdraw(raised.id);
    await flush();
    assert.deepEqual(events.map((e) => [e.kind, e.review.subject, e.review.state]), [["review", "Look at the plan", "pending"], ["review", "Look at the plan", "withdrawn"]]);

    close();
    subject.request({ subject: "Late" });
    await flush();
    assert.equal(events.length, 2, "nothing after close");
  });
}
