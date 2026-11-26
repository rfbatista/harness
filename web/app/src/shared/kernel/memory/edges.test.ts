// Edge cases of the display rules the stories name: hysteresis at the exact thresholds, the 3 s hold to the
// millisecond, rounding next to 1 GB, and the ended windows. Fake clock only: every instant is passed in.

import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { GB, MB } from "../../testing/memory-fixtures";
import { formatBudget, formatShown } from "./bytes";
import { HOLD_MS, initialTracker, settle, type Level, type Tracker } from "./level";
import { CARD_ENDED_MS, CHIP_ENDED_MS, endedBy, hasValue, shareOf, type MemoryKill, type MemorySample } from "./sample";
import { summarize } from "./summary";
import { memoryView } from "./view";

/** A tracker that has settled at `level` at t = 0. */
const at = (level: Level): Tracker => ({ level, since: 0 });
const after = (t: Tracker, share: number, ms = HOLD_MS): Level => settle(t, share, ms).level;

describe("hysteresis edges (story 3)", () => {
  test("normal enters near at 80 %, not at 79.9 %", () => {
    assert.equal(after(at("normal"), 0.799), "normal");
    assert.equal(after(at("normal"), 0.8), "near");
  });

  test("near stays near at exactly 75 % and leaves only below it", () => {
    assert.equal(after(at("near"), 0.75), "near");
    assert.equal(after(at("near"), 0.7499), "normal");
  });

  test("near enters over at 100 %, not at 99.9 %", () => {
    assert.equal(after(at("near"), 0.999), "near");
    assert.equal(after(at("near"), 1.0), "over");
  });

  test("over stays over at exactly 95 % and leaves only below it, to near", () => {
    assert.equal(after(at("over"), 0.95), "over");
    assert.equal(after(at("over"), 0.9499), "near");
  });

  test("normal reaching 99.9 % is near, and 100 % jumps straight to over", () => {
    assert.equal(after(at("normal"), 0.999), "near");
    assert.equal(after(at("normal"), 1.0), "over");
  });

  test("the 80 % entry applies from normal only: a tracker in near does not need to re-enter", () => {
    assert.equal(after(at("near"), 0.78), "near");
  });
});

describe("the 3 s hold to the millisecond (story 3)", () => {
  test("a change is refused 1 ms before 3 s and allowed at 3 s", () => {
    assert.equal(settle(at("normal"), 0.9, HOLD_MS - 1).level, "normal");
    assert.equal(settle(at("normal"), 0.9, HOLD_MS).level, "near");
  });

  test("leaving is held the same way", () => {
    assert.equal(settle(at("near"), 0.1, HOLD_MS - 1).level, "near");
    assert.equal(settle(at("near"), 0.1, HOLD_MS).level, "normal");
  });

  test("a refused change does not restart the hold", () => {
    let t = at("normal");
    t = settle(t, 0.9, 1000);
    t = settle(t, 0.9, 2000);
    assert.equal(t.since, 0);
    assert.equal(settle(t, 0.9, 3000).level, "near");
  });

  test("a change restarts the hold from its own instant", () => {
    const t = settle(at("normal"), 0.9, 3000);
    assert.equal(t.since, 3000);
    assert.equal(settle(t, 1.2, 5999).level, "near");
    assert.equal(settle(t, 1.2, 6000).level, "over");
  });

  test("a sample that asks for the same level changes nothing, not even the tracker identity", () => {
    const t = at("near");
    assert.equal(settle(t, 0.85, 10_000), t);
  });
});

describe("rounding near 1 GB (story 4)", () => {
  test("whole MB just below 1 GB, 0.1 GB from 1 GB", () => {
    assert.equal(formatShown(1 * GB - 1), "1.0 GB", "never 1024 MB");
    assert.equal(formatShown(1023.4 * MB), "1023 MB");
    assert.equal(formatShown(1 * GB), "1.0 GB");
    assert.equal(formatShown(1 * GB + 1), "1.0 GB");
  });

  test("half a tenth rounds up, below it down", () => {
    assert.equal(formatShown(1.05 * GB), "1.1 GB");
    assert.equal(formatShown(1.049 * GB), "1.0 GB");
    assert.equal(formatShown(9.95 * GB), "10.0 GB");
  });

  test("a sub-GB budget stays in MB and a whole-GB budget drops the .0", () => {
    assert.equal(formatBudget(1 * GB), "1 GB");
    assert.equal(formatBudget(1.5 * GB), "1.5 GB");
    assert.equal(formatBudget(768 * MB), "768 MB");
    assert.equal(formatBudget(10 * GB), "10 GB");
  });

  test("the sum of the bar rounds the total, not each term", () => {
    const e = (gb: number) => ({ rssBytes: gb * GB, level: "normal" as const, killedAt: [] });
    assert.equal(summarize([e(0.34), e(0.34), e(0.34)], 0).total, "1.0 GB");
    assert.equal(summarize([e(0.45), e(0.45)], 0).total, "922 MB");
  });
});

describe("a sample that says nothing (story 9)", () => {
  const sample = (over: Partial<MemorySample> = {}): MemorySample => ({
    sessionId: "s1", taskId: "t1", sampledAt: new Date(0), rssBytes: 1 * GB, budgetBytes: 4 * GB, processes: 3, heaviest: null, ...over,
  });

  test("null sampled_at, rss 0 and a missing sample have no value", () => {
    assert.equal(hasValue(null), false);
    assert.equal(hasValue(sample({ sampledAt: null })), false);
    assert.equal(hasValue(sample({ rssBytes: 0 })), false);
    assert.equal(hasValue(sample()), true);
  });

  test("a zero budget gives a share of 0, never Infinity or NaN, and a view with fill 0", () => {
    const s = sample({ budgetBytes: 0 });
    assert.equal(shareOf(s), 0);
    assert.equal(memoryView(s, "normal", null)?.fillPercent, 0);
  });

  test("the fill never passes 100 and never reads 0/4 GB", () => {
    assert.equal(memoryView(sample({ rssBytes: 40 * GB }), "over", null)?.fillPercent, 100);
    assert.equal(memoryView(sample({ rssBytes: 0 }), "normal", null), null);
  });
});

describe("ended windows at the edge (stories 8, 11, 13)", () => {
  const T = 1_000_000;
  const kill = (ms: number, pid = 1): MemoryKill => ({
    sessionId: "s1", taskId: "t1", killedAt: new Date(ms), pid, command: "node jest", rssBytes: GB, treeRssBytes: GB, budgetBytes: 4 * GB, signal: "SIGTERM",
  });

  test("chip: 59.999 s ended, 60 s back; card and bar: 4 min 59.999 s ended, 5 min back", () => {
    const k = [kill(T)];
    assert.notEqual(endedBy(k, T + CHIP_ENDED_MS - 1, CHIP_ENDED_MS), null);
    assert.equal(endedBy(k, T + CHIP_ENDED_MS, CHIP_ENDED_MS), null);
    assert.notEqual(endedBy(k, T + CARD_ENDED_MS - 1, CARD_ENDED_MS), null);
    assert.equal(endedBy(k, T + CARD_ENDED_MS, CARD_ENDED_MS), null);
  });

  test("a kill 1 ms in the future (clock skew) is not yet ended", () => {
    assert.equal(endedBy([kill(T + 1)], T, CHIP_ENDED_MS), null);
  });

  test("the bar counts a kill up to 5 min and drops it at 5 min", () => {
    const entry = () => ({ rssBytes: GB, level: "normal" as const, killedAt: [new Date(T)] });
    assert.equal(summarize([entry()], T + CARD_ENDED_MS - 1).ended, 1);
    assert.equal(summarize([entry()], T + CARD_ENDED_MS).ended, 0);
  });

  test("the bar counts the near sessions by their held level, and a session with no sample is no total", () => {
    const e = (gb: number | null, level: Level) => ({ rssBytes: gb === null ? null : gb * GB, level, killedAt: [] });
    const s = summarize([e(3.4, "near"), e(3.5, "near"), e(1, "normal"), e(null, "near")], T);
    assert.deepEqual([s.sampled, s.near, s.total], [3, 2, "7.9 GB"]);
  });

  test("a tracker never leaves its initial state on nothing: initialTracker is normal and free to change", () => {
    assert.equal(settle(initialTracker(), 0.9, 0).level, "near");
  });
});
