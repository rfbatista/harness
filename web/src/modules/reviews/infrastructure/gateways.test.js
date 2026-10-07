// Both ReviewGateway implementations run the same contract.

import { Codes } from "../../../shared/domain/errors.js";
import { apiClient } from "../../../shared/infrastructure/api.js";
import { feed } from "../../../shared/infrastructure/feed.js";
import { flush } from "../../../shared/testing/doubles.js";
import { assert, file, test } from "../../../shared/testing/test.js";
import { reviewGatewayContract } from "../testing/gateway-contract.js";
import { makeReview, reviewDTO } from "../testing/fixtures.js";
import { stubReviewsApi } from "../testing/stub-api.js";
import { toResponse, toReview, toReviewEvent, toReviews } from "./dto.js";
import { memoryReviews } from "./memory-gateway.js";
import { reviewsGateway } from "./reviews-gateway.js";

file("reviews/infrastructure/gateways");

reviewGatewayContract("memory", (world) => {
  const memory = memoryReviews(world);
  return { gateway: memory.gateway, request: memory.request, withdraw: memory.withdraw };
});

function http(world) {
  const stub = stubReviewsApi(world);
  const gateway = reviewsGateway(apiClient({ base: "/api", fetch: stub.fetch }), feed({ base: "/api", EventSource: stub.EventSource }));
  return { gateway, stub };
}

reviewGatewayContract("http", (world) => {
  const { gateway, stub } = http(world);
  return { gateway, request: stub.memory.request, withdraw: stub.memory.withdraw };
});

test("http · a malformed review request is dropped, the stream stays open", async () => {
  const { gateway, stub } = http({});
  const events = [];
  const close = gateway.follow("p1", (e) => events.push(e), () => {});
  await flush();
  stub.pushRaw({ review_request: { id: "", state: "pending" } });
  stub.pushRaw({ task_message: { id: "m1" } });
  stub.pushRaw({ artifact: { id: "a1", kind: "page", revision: 1, scope: "project", attached_ticket_ids: ["t2"], updated_at: "2026-10-07T12:00:00Z" } });
  stub.pushRaw({ artifact: { id: "a1" }, deleted: true });
  stub.memory.request({ subject: "After" });
  await flush();
  assert.deepEqual(events.map((e) => e.review.subject), ["After"]);
  close();
});

test("a review request reads from the feed's keyed shape and a session stream's typed one", () => {
  const dto = reviewDTO(makeReview({ id: "rv7" }));
  assert.equal(toReviewEvent({ review_request: dto }).review.id, "rv7");
  assert.equal(toReviewEvent({ type: "review_request", review_request: dto }).review.id, "rv7");
  assert.equal(toReviewEvent({ ticket: { id: "t1" } }), null);
  assert.equal(toReviewEvent({ type: "artifact", review_request: dto }), null);
});

test("a review request without its optional fields reads them as empty; its artifacts get their view", () => {
  const r = toReview({ id: "rv1", state: "pending", created_at: "2026-10-02T14:00:00Z", subject: "S", artifact_ids: ["x/y"], document_ids: null });
  assert.deepEqual([r.aboutSessionId, r.responseNote, r.respondedAt, r.documentIds, r.updatedAt.getTime()], ["", "", null, [], r.createdAt.getTime()]);
  assert.deepEqual(r.artifacts, [{ id: "x/y", href: "/api/artifacts/x%2Fy/view/" }]);
  assert.ok(Object.isFrozen(r));
});

test("a malformed review request or answer is BAD_RESPONSE", () => {
  const dto = reviewDTO(makeReview());
  assert.throws(() => toReview({ ...dto, id: "" }), Codes.BAD_RESPONSE);
  assert.throws(() => toReview({ ...dto, state: "pondering" }), Codes.BAD_RESPONSE);
  assert.throws(() => toReview({ ...dto, created_at: "" }), Codes.BAD_RESPONSE);
  assert.throws(() => toReviews({}), Codes.BAD_RESPONSE);
  assert.throws(() => toResponse({ delivered: true }), Codes.BAD_RESPONSE);
  assert.equal(toResponse({ review_request: dto }).delivered, false, "delivered only when the server says so");
});
