// The contract every ScoreSource adapter keeps: the bench API's cases, scorecards (newest first) and the run it starts.

import { describe, test } from "node:test";
import assert from "node:assert/strict";
import type { ScoreSource } from "../application/ports";
import { ScoreError } from "../domain/bench";
import type { FakeBench } from "./fake-bench";

export interface ScoreSourceHarness {
  source: ScoreSource;
  bench: FakeBench;
}

export function scoreSourceContract(name: string, make: () => ScoreSourceHarness): void {
  describe(`ScoreSource contract: ${name}`, () => {
    test("a project with nothing yet has no cases and no scorecards", async () => {
      const { source } = make();
      assert.deepEqual(await source.cases("p1"), []);
      assert.deepEqual(await source.scorecards("p1"), []);
    });

    test("the cases name their mock tools", async () => {
      const { source, bench } = make();
      bench.cases.push({ id: "c1", name: "brief", prompt: "delegate it", tools: [{ name: "create_task" }, { name: "message_architect" }] });
      assert.deepEqual(await source.cases("p1"), [{ id: "c1", name: "brief", prompt: "delegate it", tools: ["create_task", "message_architect"], source: "definition" }]);
    });

    test("scorecards come newest first, with their scores, cost and setup", async () => {
      const { source, bench } = make();
      bench.scorecards.push({ id: "s1", at: "2026-11-25T10:00:00Z", cost: 1.09, cases: [{ id: "c1", tool: 1, outcome: 0.5 }] }, { id: "s2", at: "2026-11-26T10:00:00Z", cases: [{ id: "c1", tool: 0.5 }] });
      const cards = await source.scorecards("p1");
      assert.deepEqual(cards.map((c) => c.id), ["s2", "s1"]);
      assert.equal(cards[1]?.costUsd, 1.09);
      assert.equal(cards[1]?.setup.fingerprint, "fp1");
      assert.equal(cards[1]?.aggregate.mean, 0.75);
      assert.equal(cards[1]?.cases[0]?.patterns.outcome.scored, true);
      assert.equal(cards[0]?.cases[0]?.patterns.outcome.scored, false);
    });

    test("starting a run answers it running, with the cases it will score", async () => {
      const { source, bench } = make();
      bench.cases.push({ id: "c1", prompt: "p", tools: [] });
      const run = await source.startRun("p1");
      assert.deepEqual([run.status, run.total, run.done], ["running", 1, 0]);
    });

    test("a refused run fails with the message", async () => {
      const { source, bench } = make();
      bench.refuseRun("add a case first");
      await assert.rejects(source.startRun("p1"), (err: unknown) => err instanceof ScoreError && /add a case first/.test(err.message));
    });

    test("a harness without the bench is UNAVAILABLE, and a dropped network is NETWORK_ERROR", async () => {
      const off = make();
      off.bench.switchOff();
      await assert.rejects(off.source.cases("p1"), (err: unknown) => err instanceof ScoreError && err.code === "UNAVAILABLE");
      const down = make();
      down.bench.breakNetwork();
      await assert.rejects(down.source.scorecards("p1"), (err: unknown) => err instanceof ScoreError && err.code === "NETWORK_ERROR");
    });
  });
}
