// Both RailGateway implementations run the same contract.

import { Codes } from "../../../shared/domain/errors.js";
import { apiClient } from "../../../shared/infrastructure/api.js";
import { feed } from "../../../shared/infrastructure/feed.js";
import { jsonResponse } from "../../../shared/testing/doubles.js";
import { assert, file, test } from "../../../shared/testing/test.js";
import { makeTask, taskDTO } from "../testing/fixtures.js";
import { railGatewayContract } from "../testing/rail-contract.js";
import { memoryRail } from "./memory-rail.js";
import { railGateway, toProjectChange, toRailChange } from "./rail-gateway.js";

file("tasks/infrastructure/rail");

const toDTO = (s) => ({ id: s.id, ticket_id: s.ticketId || undefined, status: s.status, pending_approvals: s.pendingApprovals });

railGatewayContract("memory", (world) => {
  const m = memoryRail(world);
  return { gateway: m.gateway, emit: m.emit, seed: { project_id: world.projectId, sessions: world.sessions, tasks: world.tasks.map(taskDTO) } };
});

railGatewayContract("http", (world) => {
  const memory = memoryRail(world);
  const fetch = async (input) => {
    const url = new URL(input, "http://harness.test");
    if (url.pathname === "/api/sessions") return jsonResponse(200, { sessions: (await memory.gateway.listSessions()).map(toDTO) });
    if (url.pathname === "/api/list_tickets") return jsonResponse(200, { tickets: (await memory.gateway.listTasks()).map(taskDTO) });
    return jsonResponse(404, { error: "no route" });
  };
  class StubEventSource {
    constructor(input) {
      const url = new URL(input, "http://harness.test");
      this.unfollow = memory.gateway.follow(url.searchParams.get("project_id"), (c) => {
        const data =
          c.kind === "deleted" ? { session: { id: c.id }, deleted: true }
          : c.kind === "upsert" ? { session: toDTO(c.session) }
          : c.kind === "task-deleted" ? { ticket: { id: c.id }, deleted: true }
          : { ticket: taskDTO(c.task) };
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
    seed: { project_id: world.projectId, sessions: world.sessions.map(toDTO), tasks: world.tasks.map(taskDTO) },
  };
});

test("a malformed feed change is refused with BAD_RESPONSE", () => {
  assert.throws(() => toRailChange({ session: { status: "running" } }), Codes.BAD_RESPONSE);
});

test("a ticket message becomes a task change; a deleted one carries only the id", () => {
  assert.deepEqual(toProjectChange({ ticket: taskDTO(makeTask()) }), { kind: "task-upsert", task: makeTask() });
  assert.deepEqual(toProjectChange({ ticket: { id: "t1" }, deleted: true }), { kind: "task-deleted", id: "t1" });
});

test("a message of a kind it does not know, or one it cannot read, is dropped, not thrown", () => {
  assert.equal(toProjectChange({ run: { id: "r1" } }), null);
  assert.equal(toProjectChange({}), null);
  assert.equal(toProjectChange({ ticket: { id: "t1", status: "someday" } }), null, "an unknown status drops the message (Review Focus 1)");
  assert.equal(toProjectChange({ ticket: { status: "todo" } }), null);
});

test("listing tasks leaves out one it cannot read instead of failing the whole list", async () => {
  const fetch = async () => jsonResponse(200, { tickets: [taskDTO(makeTask()), { id: "t-odd", project_id: "p1", title: "Someday", status: "someday" }] });
  const gateway = railGateway(apiClient({ base: "/api", fetch }), feed({ base: "/api", EventSource: class {} }));
  assert.deepEqual((await gateway.listTasks("p1")).map((t) => t.id), ["t1"]);
});
