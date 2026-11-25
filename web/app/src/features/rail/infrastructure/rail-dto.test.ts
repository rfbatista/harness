import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { memoryFeed } from "../../../shared/infrastructure/memory-feed";
import type { ApiClient } from "../../../shared/infrastructure/api-client";
import type { RailChange } from "../application/ports";
import { session, sessionWire, task, taskWire } from "../testing/fixtures";
import { RailSeedDto } from "./dto/rail-seed-dto";
import { httpRailFeed, projectFeedPath, toRailChange } from "./http-rail-feed";
import { httpRailSource } from "./http-rail-source";
import { toSeed } from "./rail-mapper";

describe("the rail seed", () => {
  test("decodes tasks, sessions and the archived count with the same meanings the Alpine decoder gave them", () => {
    const wire = { project_id: "p1", archived_count: 2, sessions: [sessionWire(session("a", "t1", "idle", { pendingApprovals: 1 }))], tasks: [{ ...taskWire(task({ pendingReviews: 2, updatedAt: 1000 })), status_since: "", created_at: "2026-01-02T00:00:00Z", merge_state: { state: "partial", merged: 1, total: 2 } }] };
    const seed = toSeed(RailSeedDto.fromJSON(wire));
    assert.equal(seed.projectId, "p1");
    assert.equal(seed.archivedCount, 2);
    assert.deepEqual(seed.sessions, [{ id: "a", ticketId: "t1", status: "idle", pendingApprovals: 1, openInterviewCount: 0 }]);
    const [t] = seed.tasks;
    assert.deepEqual([t?.pendingReviews, t?.updatedAt, t?.statusSince, t?.archivedAt, t?.mergeState], [2, 1000, Date.parse("2026-01-02T00:00:00Z"), 0, { state: "partial", merged: 1, total: 2 }]);
  });

  test("leaves out a task of a status it does not know, reads the rest, and does not throw on missing optional fields", () => {
    const seed = toSeed(RailSeedDto.fromJSON({ project_id: "p1", tasks: [{ id: "x", status: "someday" }, { id: "t2", status: "todo" }], sessions: [] }));
    assert.deepEqual(seed.tasks.map((t) => t.id), ["t2"]);
    assert.equal(seed.archivedCount, 0);
  });

  test("a seed without a project is refused", () => {
    assert.throws(() => RailSeedDto.fromJSON({ tasks: [] }), /project_id/);
  });

  test("the server's zero time reads as unknown", () => {
    const [t] = toSeed(RailSeedDto.fromJSON({ project_id: "p1", tasks: [{ id: "t", status: "todo", status_since: "0001-01-01T00:00:00Z", archived_at: "0001-01-01T00:00:00Z" }] })).tasks;
    assert.deepEqual([t?.statusSince, t?.archivedAt], [0, 0]);
  });
});

describe("the project feed's changes", () => {
  const read = (data: unknown): RailChange | null => toRailChange(data);

  test("ticket and session changes decode; deleted ones carry the id only", () => {
    assert.deepEqual(read({ ticket: taskWire(task({ status: "review" })) }), { kind: "task-upsert", task: { ...task({ status: "review" }), statusSince: Date.parse("2026-01-01T00:00:00Z") } });
    assert.deepEqual(read({ ticket: { id: "t1", status: "todo" }, deleted: true }), { kind: "task-deleted", id: "t1" });
    assert.deepEqual(read({ session: sessionWire(session("a", "t1", "running")) }), { kind: "upsert", session: session("a", "t1", "running") });
    assert.deepEqual(read({ session: { id: "a", status: "done" }, deleted: true }), { kind: "deleted", id: "a" });
  });

  test("a task_status, an unknown kind or a change that does not read is not the rail's", () => {
    assert.equal(read({ task_status: { task_id: "t1", by: "person" } }), null);
    assert.equal(read({ context: 1 }), null);
    assert.equal(read({ ticket: { id: "t1", status: "someday" } }), null);
    assert.equal(read("text"), null);
    assert.equal(read(null), null);
  });

  test("the adapter follows the project's path on the shared stream, passes status on, and stops", () => {
    const stream = memoryFeed();
    const heard: RailChange[] = [];
    const statuses: string[] = [];
    const stop = httpRailFeed(stream).follow("p 1", { onChange: (c) => heard.push(c), onStatus: (s) => statuses.push(s) });
    const path = projectFeedPath("p 1");
    stream.emit(path, { session: sessionWire(session("a", "t1", "running")) });
    stream.emit(path, { task_status: {} });
    stream.setStatus(path, "live");
    assert.deepEqual([heard.length, statuses], [1, ["connecting", "live"]]);
    stop();
    assert.equal(stream.followers(path), 0);
  });
});

describe("the lists read again", () => {
  function api(answers: Record<string, unknown>): ApiClient & { asked: string[] } {
    const asked: string[] = [];
    return {
      asked,
      async get(path, query) {
        asked.push(`${path}?${new URLSearchParams(query).toString()}`);
        const answer = answers[path + (query?.["archived"] ? "#archived" : "")];
        if (answer === undefined) throw new Error("no answer");
        return answer;
      },
      post: async () => undefined,
    };
  }

  test("reads GET /sessions and GET /list_tickets, archived ones with archived=true", async () => {
    const client = api({ "/sessions": { sessions: [sessionWire(session("a", "t1", "running"))] }, "/list_tickets": { tickets: [taskWire(task())] }, "/list_tickets#archived": { tickets: [taskWire(task({ id: "o", archivedAt: 5 }))] } });
    const source = httpRailSource(client);
    assert.deepEqual((await source.sessions("p1")).map((s) => s.id), ["a"]);
    assert.deepEqual((await source.tasks("p1")).map((t) => t.id), ["t1"]);
    assert.deepEqual((await source.archived("p1")).map((t) => t.id), ["o"]);
    assert.deepEqual(client.asked, ["/sessions?project_id=p1", "/list_tickets?project_id=p1", "/list_tickets?project_id=p1&archived=true"]);
  });

  test("an answer without its list is a BAD_RESPONSE", async () => {
    const source = httpRailSource(api({ "/sessions": {}, "/list_tickets": { tickets: "no" } }));
    await assert.rejects(source.sessions("p1"), { code: "BAD_RESPONSE" });
    await assert.rejects(source.tasks("p1"), { code: "BAD_RESPONSE" });
  });
});
