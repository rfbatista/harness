import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { GB } from "../../testing/memory-fixtures";
import { summarize } from "./summary";

const NOW = Date.parse("2026-11-25T22:50:00Z");
const entry = (gb: number | null, level: "normal" | "near" | "over", killedAgoMs: number[] = []) => ({
  rssBytes: gb === null ? null : gb * GB,
  level,
  killedAt: killedAgoMs.map((ms) => new Date(NOW - ms)),
});

describe("memory summary of a task's live sessions", () => {
  test("sums the last values and counts a badge per state present", () => {
    const s = summarize([entry(3.4, "near"), entry(1.2, "normal"), entry(6.1, "normal", [60_000])], NOW);
    assert.equal(s.total, "10.7 GB");
    assert.deepEqual([s.near, s.ended], [1, 1]);
  });

  test("kills count for 5 min, then drop; a session may count several kills", () => {
    assert.equal(summarize([entry(1, "normal", [299_000, 10_000])], NOW).ended, 2);
    assert.equal(summarize([entry(1, "normal", [300_000])], NOW).ended, 0);
  });

  test("sessions without a sample do not count, and nothing present gives no badges", () => {
    const s = summarize([entry(null, "normal"), entry(0, "normal")], NOW);
    assert.deepEqual([s.total, s.near, s.ended, s.sampled], [null, 0, 0, 0]);
  });

  test("below 1 GB the total is whole MB", () => {
    assert.equal(summarize([entry(0.25, "normal")], NOW).total, "256 MB");
  });
});
