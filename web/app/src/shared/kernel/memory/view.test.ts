import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { GB, MB } from "../../testing/memory-fixtures";
import { CARD_ENDED_MS, CHIP_ENDED_MS, endedBy, killKey, type MemoryKill, type MemorySample } from "./sample";
import { memoryView, sameView } from "./view";

const sample = (over: Partial<MemorySample> = {}): MemorySample => ({
  sessionId: "s1",
  taskId: "t1",
  sampledAt: new Date("2026-11-25T22:40:01Z"),
  rssBytes: 3.4 * GB,
  budgetBytes: 4 * GB,
  processes: 9,
  heaviest: { pid: 1, command: "node jest", rssBytes: 905 * MB },
  ...over,
});
const kill = (over: Partial<MemoryKill> = {}): MemoryKill => ({
  sessionId: "s1",
  taskId: "t1",
  killedAt: new Date("2026-11-25T22:40:03Z"),
  pid: 81234,
  command: "node jest",
  rssBytes: 3.5 * GB,
  treeRssBytes: 4.4 * GB,
  budgetBytes: 4 * GB,
  signal: "SIGTERM",
  ...over,
});

describe("memory view", () => {
  test("normal says no word, the near sentence names the state and the process count", () => {
    const normal = memoryView(sample({ rssBytes: 1.2 * GB }), "normal", null)!;
    assert.deepEqual([normal.state, normal.num, normal.of, normal.word], ["normal", "1.2", " / 4 GB", ""]);
    assert.equal(normal.title, "Memory: 1.2 GB of the 4 GB budget · 9 processes");
    const near = memoryView(sample(), "near", null)!;
    assert.equal(near.word, "near budget");
    assert.equal(near.fillPercent, 85);
    assert.equal(near.title, "Memory: 3.4 GB of the 4 GB budget · near budget · 9 processes");
  });

  test("over fills to 100 and below 1 GB the number carries its unit", () => {
    const over = memoryView(sample({ rssBytes: 4.5 * GB }), "over", null)!;
    assert.deepEqual([over.word, over.fillPercent], ["over budget", 100]);
    assert.equal(memoryView(sample({ rssBytes: 905 * MB }), "normal", null)!.num, "905 MB");
    assert.match(memoryView(sample({ processes: 1 }), "normal", null)!.title, /· 1 process$/);
  });

  test("ended overrides the sample states: the word is the command, the fill stays at the current value", () => {
    const view = memoryView(sample(), "near", kill())!;
    assert.deepEqual([view.state, view.word, view.fillPercent], ["ended", "node jest ended", 85]);
    assert.equal(view.title, "Memory: 3.4 GB of the 4 GB budget · node jest ended · 9 processes");
  });

  test("no value, no view: before the first sample and at rss 0", () => {
    assert.equal(memoryView(sample({ sampledAt: null, rssBytes: 0, processes: 0 }), "normal", null), null);
    assert.equal(memoryView(sample({ rssBytes: 0, processes: 0 }), "normal", null), null);
    assert.equal(memoryView(null, "normal", null), null);
  });
});

describe("same view", () => {
  test("views compare by what they show; null only equals null", () => {
    const a = memoryView(sample(), "near", null);
    assert.equal(sameView(a, memoryView(sample({ rssBytes: 3.4 * GB + 1 }), "near", null)), true);
    assert.equal(sameView(a, memoryView(sample(), "normal", null)), false);
    assert.equal(sameView(a, null), false);
    assert.equal(sameView(null, null), true);
  });
});

describe("ended windows and kill keys", () => {
  const t0 = new Date("2026-11-25T22:40:03Z").getTime();

  test("a kill ends the chip for 60 s and the card for 5 min, then the samples come back", () => {
    assert.equal(CHIP_ENDED_MS, 60_000);
    assert.equal(CARD_ENDED_MS, 300_000);
    const kills = [kill()];
    assert.equal(endedBy(kills, t0 + 59_999, CHIP_ENDED_MS), kills[0]);
    assert.equal(endedBy(kills, t0 + 60_000, CHIP_ENDED_MS), null);
    assert.equal(endedBy(kills, t0 + 299_999, CARD_ENDED_MS), kills[0]);
    assert.equal(endedBy(kills, t0 + 300_000, CARD_ENDED_MS), null);
  });

  test("the latest kill in the window wins; none gives null", () => {
    const later = kill({ pid: 2, killedAt: new Date(t0 + 10_000) });
    assert.equal(endedBy([kill(), later], t0 + 20_000, CHIP_ENDED_MS), later);
    assert.equal(endedBy([], t0, CHIP_ENDED_MS), null);
  });

  test("a kill is keyed by session, pid and time", () => {
    assert.equal(killKey(kill()), killKey(kill()));
    assert.notEqual(killKey(kill()), killKey(kill({ pid: 9 })));
    assert.notEqual(killKey(kill()), killKey(kill({ sessionId: "s2" })));
    assert.notEqual(killKey(kill()), killKey(kill({ killedAt: new Date(t0 + 1000) })));
  });
});
