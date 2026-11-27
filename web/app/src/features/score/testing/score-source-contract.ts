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
      const [one] = await source.cases("p1");
      assert.deepEqual(one?.tools, ["create_task", "message_architect"]);
      assert.equal(one?.source, "definition");
      assert.deepEqual(one?.expect, { tools: [], ordered: false, reference: "" });
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

    test("a scorecard is compared with the previous finished run, or with the baseline asked for", async () => {
      const { source, bench } = make();
      bench.scorecards.push(
        { id: "s1", at: "2026-11-24T10:00:00Z", cases: [{ id: "c1", tool: 1, outcome: 1 }] },
        { id: "s2", at: "2026-11-25T10:00:00Z", status: "partial", cases: [{ id: "c1", tool: 0 }] },
        { id: "s3", at: "2026-11-26T10:00:00Z", cases: [{ id: "c1", tool: 0.5, outcome: 0.5 }, { id: "c2", tool: 1 }] },
      );
      const byDefault = await source.compare("p1", "s3", null);
      assert.equal(byDefault?.previousId, "s1", "a partial run is never the default baseline");
      assert.equal(byDefault?.aggregate.change, -0.25);
      assert.equal(byDefault?.cases.find((c) => c.caseId === "c1")?.mean?.change, -0.5);
      assert.equal(byDefault?.cases.find((c) => c.caseId === "c2")?.status, "added");
      assert.ok(byDefault?.groups.tool_use);
      const chosen = await source.compare("p1", "s3", "s2");
      assert.equal(chosen?.previousId, "s2");
    });

    test("the first scorecard has nothing to compare with", async () => {
      const { source, bench } = make();
      bench.scorecards.push({ id: "s1", at: "2026-11-24T10:00:00Z", cases: [{ id: "c1", tool: 1 }] });
      assert.equal(await source.compare("p1", "s1", null), null);
    });

    test("a run can be asked for some of the cases only; an unknown one is refused naming it", async () => {
      const { source, bench } = make();
      bench.cases.push({ id: "c1", prompt: "p", tools: [] }, { id: "c2", prompt: "q", tools: [] });
      assert.equal((await source.startRun("p1", ["c2"])).total, 1);
      await assert.rejects(source.startRun("p1", ["ghost"]), (err: unknown) => err instanceof ScoreError && /ghost/.test(err.message));
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

    test("the case's mocks and expectation are read whole", async () => {
      const { source, bench } = make();
      bench.cases.push({ id: "c1", prompt: "p", tools: [{ name: "read_file", result: "# Demo", is_error: true }], expect: { tools: [{ tool: "read_file", params: { path: "README.md" } }, { tool: "read_file" }], ordered: true, reference: "A demo." } });
      const [one] = await source.cases("p1");
      assert.deepEqual(one?.mocks, [{ name: "read_file", description: "", result: "# Demo", isError: true }]);
      assert.deepEqual(one?.expect, { tools: [{ tool: "read_file", params: { path: "README.md" } }, { tool: "read_file", params: null }], ordered: true, reference: "A demo." });
    });

    test("a scorecard keeps the trace of a case: calls, the judge's note per step, reasons and the final text", async () => {
      const { source, bench } = make();
      bench.scorecards.push({ id: "s1", at: "2026-11-26T10:00:00Z", cases: [{ id: "c1", trajectory: 0.4, calls: [{ tool: "read_file", params: { path: "a" } }], steps: [{ step: 1, ok: false, note: "early" }], reasons: ["called write_file before reading"], finalText: "done" }] });
      const [card] = await source.scorecards("p1");
      const scored = card?.cases[0];
      assert.deepEqual(scored?.calls, [{ tool: "read_file", params: { path: "a" } }]);
      assert.deepEqual(scored?.steps, [{ step: 1, ok: false, note: "early" }]);
      assert.deepEqual(scored?.reasons.trajectory, ["called write_file before reading"]);
      assert.equal(scored?.finalText, "done");
    });

    test("a case is created from a definition and listed", async () => {
      const { source } = make();
      const made = await source.createCase("p1", { name: "reads", prompt: "Read it", mocks: [{ name: "read_file", description: "", result: "ok", isError: false }], expect: { tools: [{ tool: "read_file", params: null }], ordered: false, reference: "" } });
      assert.equal(made.name, "reads");
      assert.deepEqual((await source.cases("p1")).map((c) => c.id), [made.id]);
    });

    test("a refused definition names its fields and stores nothing", async () => {
      const { source } = make();
      await assert.rejects(
        source.createCase("p1", { name: "", prompt: "", mocks: [], expect: { tools: [], ordered: false, reference: "" } }),
        (err: unknown) => err instanceof ScoreError && err.code === "REFUSED" && err.fields.some((f) => f.field === "prompt" && f.code === "REQUIRED") && err.fields.some((f) => f.field === "expect" && f.code === "NO_EXPECTATION"),
      );
      assert.deepEqual(await source.cases("p1"), []);
    });

    test("an expectation is replaced, and a refused one leaves the case as it was", async () => {
      const { source, bench } = make();
      bench.cases.push({ id: "c1", prompt: "p", tools: [{ name: "read_file", result: "x" }], expect: { reference: "old" } });
      const edited = await source.updateExpectation("p1", "c1", { tools: [{ tool: "read_file", params: null }], ordered: true, reference: "new" });
      assert.deepEqual(edited.expect, { tools: [{ tool: "read_file", params: null }], ordered: true, reference: "new" });
      await assert.rejects(source.updateExpectation("p1", "c1", { tools: [{ tool: "nope", params: null }], ordered: false, reference: "" }), (err: unknown) => err instanceof ScoreError && err.code === "REFUSED" && err.fields[0]?.field === "expect.tools[0].tool");
      assert.equal((await source.cases("p1"))[0]?.expect.reference, "new");
      await assert.rejects(source.updateExpectation("p1", "gone", { tools: [], ordered: false, reference: "r" }), (err: unknown) => err instanceof ScoreError && err.code === "NOT_FOUND");
    });

    test("a case is deleted; an unknown one is not found", async () => {
      const { source, bench } = make();
      bench.cases.push({ id: "c1", prompt: "p", tools: [] });
      await source.deleteCase("p1", "c1");
      assert.deepEqual(await source.cases("p1"), []);
      await assert.rejects(source.deleteCase("p1", "c1"), (err: unknown) => err instanceof ScoreError && err.code === "NOT_FOUND");
    });

    test("recorded trajectories are listed and one becomes a case", async () => {
      const { source, bench } = make();
      bench.trajectories.push({ id: "t1", task_id: "task1", prompt: "Fix the bug", steps: [{}, {}], recorded_at: "2026-11-26T10:00:00Z" });
      const [t] = await source.trajectories("p1");
      assert.deepEqual({ id: t?.id, taskId: t?.taskId, prompt: t?.prompt, steps: t?.steps }, { id: "t1", taskId: "task1", prompt: "Fix the bug", steps: 2 });
      const made = await source.caseFromTrajectory("p1", "t1");
      assert.equal(made.source, "trajectory");
      assert.equal(made.prompt, "Fix the bug");
    });
  });
}
