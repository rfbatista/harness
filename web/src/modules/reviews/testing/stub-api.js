// A stand-in for the harness server's review routes, backed by the memory
// gateway: a fetch and an EventSource that speak the Web UI contract's wire
// format. The real reviewsGateway runs the contract suite against it.

import { codeOf } from "../../../shared/domain/errors.js";
import { jsonResponse } from "../../../shared/testing/doubles.js";
import { memoryReviews } from "../infrastructure/memory-gateway.js";
import { reviewDTO } from "./fixtures.js";

const STATUS = { REVIEW_NOT_FOUND: 404, REVIEW_NOT_PENDING: 409, INVALID_INPUT: 400 };

export function stubReviewsApi(world) {
  const memory = memoryReviews(world);
  const { gateway } = memory;
  const sources = new Set();

  const fail = (err) => jsonResponse(STATUS[codeOf(err)] ?? 500, { error: err.message, code: codeOf(err) });

  async function fetch(input, init = {}) {
    const url = new URL(input, "http://harness.test");
    const method = init.method ?? "GET";
    const body = init.body ? JSON.parse(init.body) : {};
    try {
      if (method === "GET" && url.pathname === "/api/review_requests") {
        const ticket = url.searchParams.get("ticket_id");
        const project = url.searchParams.get("project_id");
        if (!ticket && !project) return jsonResponse(400, { error: "ticket_id or project_id is required", code: "INVALID_INPUT" });
        if (project && url.searchParams.get("state") !== "pending") return jsonResponse(400, { error: "the stub serves a project's pending ones", code: "INVALID_INPUT" });
        const list = ticket ? await gateway.listForTask(ticket) : await gateway.listPendingForProject(project);
        return jsonResponse(200, { review_requests: list.map(reviewDTO) });
      }
      if (method === "POST" && url.pathname === "/api/respond_review_request") {
        const { review, delivered } = await gateway.respond({ reviewId: body.review_id, decision: body.decision, note: body.note ?? "" });
        return jsonResponse(200, { review_request: reviewDTO(review), delivered });
      }
      return jsonResponse(404, { error: `no route ${method} ${url.pathname}` });
    } catch (err) {
      return fail(err);
    }
  }

  // The project feed carries every kind of change; the gateway must pick out
  // the review requests and ignore the rest.
  class StubEventSource {
    constructor(input) {
      const url = new URL(input, "http://harness.test");
      this.onopen = null;
      this.onmessage = null;
      this.onerror = null;
      sources.add(this);
      this.unfollow = gateway.follow(url.searchParams.get("project_id") ?? "", (event) => this.send({ review_request: reviewDTO(event.review) }), () => {});
      queueMicrotask(() => {
        this.onopen?.({});
        this.send({ ticket: { id: "t1", project_id: "p1", title: "Other news", status: "review", pending_reviews: 1 } });
      });
    }
    send(dto) {
      this.onmessage?.({ data: typeof dto === "string" ? dto : JSON.stringify(dto) });
    }
    close() {
      sources.delete(this);
      this.unfollow();
    }
  }

  return {
    fetch,
    EventSource: StubEventSource,
    memory,
    pushRaw(dto) {
      for (const s of sources) s.send(dto);
    },
  };
}
