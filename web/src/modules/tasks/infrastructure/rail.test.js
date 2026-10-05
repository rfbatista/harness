// Both RailGateway implementations run the same contract.

import { Codes } from "../../../shared/domain/errors.js";
import { apiClient } from "../../../shared/infrastructure/api.js";
import { feed } from "../../../shared/infrastructure/feed.js";
import { jsonResponse } from "../../../shared/testing/doubles.js";
import { assert, file, test } from "../../../shared/testing/test.js";
import { railGatewayContract } from "../testing/rail-contract.js";
import { memoryRail } from "./memory-rail.js";
import { railGateway, toRailChange } from "./rail-gateway.js";

file("tasks/infrastructure/rail");

const toDTO = (s) => ({ id: s.id, ticket_id: s.ticketId || undefined, status: s.status, pending_approvals: s.pendingApprovals });

railGatewayContract("memory", (world) => {
  const m = memoryRail(world);
  return { gateway: m.gateway, emit: m.emit, seed: { project_id: world.projectId, sessions: world.sessions } };
});

railGatewayContract("http", (world) => {
  const memory = memoryRail(world);
  const fetch = async (input) => {
    const url = new URL(input, "http://harness.test");
    if (url.pathname === "/api/sessions") return jsonResponse(200, { sessions: (await memory.gateway.listSessions()).map(toDTO) });
    return jsonResponse(404, { error: "no route" });
  };
  class StubEventSource {
    constructor(input) {
      const url = new URL(input, "http://harness.test");
      this.unfollow = memory.gateway.follow(url.searchParams.get("project_id"), (c) => {
        const data = c.kind === "deleted" ? { session: { id: c.id }, deleted: true } : { session: toDTO(c.session) };
        this.onmessage?.({ data: JSON.stringify(data) });
      }, () => {});
      queueMicrotask(() => this.onopen?.({}));
    }
    close() {
      this.unfollow();
    }
  }
  return {
    gateway: railGateway(apiClient({ base: "/api", fetch }), feed({ base: "/api", EventSource: StubEventSource })),
    emit: memory.emit,
    seed: { project_id: world.projectId, sessions: world.sessions.map(toDTO) },
  };
});

test("a malformed feed change is refused with BAD_RESPONSE", () => {
  assert.throws(() => toRailChange({ session: { status: "running" } }), Codes.BAD_RESPONSE);
});
