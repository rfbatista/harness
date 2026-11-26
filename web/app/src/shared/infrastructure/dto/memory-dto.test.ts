import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { wireKill, wireRead, wireSample, GB } from "../../testing/memory-fixtures";
import { DtoError } from "./dto-error";
import { MemoryKillDto, MemoryReadDto, MemorySampleDto } from "./memory-dto";

describe("memory DTOs", () => {
  test("a session.memory sample decodes with its heaviest process", () => {
    const s = MemorySampleDto.fromJSON(wireSample()).toDomain();
    assert.deepEqual([s.sessionId, s.rssBytes, s.budgetBytes, s.processes], ["e7bb0000-0000-4000-8000-000000000001", 1288490188, 4 * GB, 7]);
    assert.deepEqual(s.heaviest, { pid: 81234, command: "node jest", rssBytes: 905969664 });
    assert.equal(s.sampledAt?.toISOString(), "2026-11-25T22:40:01.000Z");
  });

  test("heaviest may be null and a sample before the first one has sampled_at null", () => {
    assert.equal(MemorySampleDto.fromJSON(wireSample({ heaviest: null })).toDomain().heaviest, null);
    const first = MemorySampleDto.fromJSON(wireSample({ sampled_at: null, rss_bytes: 0, processes: 0, heaviest: null })).toDomain();
    assert.equal(first.sampledAt, null);
  });

  test("a kill decodes with its signal; an unknown signal is refused", () => {
    const k = MemoryKillDto.fromJSON(wireKill({ signal: "SIGKILL" })).toDomain();
    assert.deepEqual([k.command, k.pid, k.treeRssBytes, k.signal], ["node jest", 81234, 4400000000, "SIGKILL"]);
    assert.throws(() => MemoryKillDto.fromJSON(wireKill({ signal: "SIGHUP" })), DtoError);
  });

  test("the initial read adds kills, the budget source and the prevention values", () => {
    const r = MemoryReadDto.fromJSON(wireRead({ kills: [wireKill()], budget_source: "task" }));
    assert.equal(r.sample.processes, 7);
    assert.equal(r.kills.length, 1);
    assert.deepEqual([r.budgetSource, r.prevention.heapMb, r.prevention.testWorkers], ["task", 1365, 2]);
    assert.throws(() => MemoryReadDto.fromJSON(wireRead({ budget_source: "elsewhere" })), DtoError);
  });
});
