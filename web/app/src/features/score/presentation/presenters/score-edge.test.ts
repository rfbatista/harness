import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { apiClient, type FetchLike } from "../../../../shared/infrastructure/api-client";
import { memoryFeed } from "../../../../shared/infrastructure/memory-feed";
import { httpScoreFeed } from "../../infrastructure/http-score-feed";
import { httpScoreSource } from "../../infrastructure/http-score-source";
import { jsonCallsText } from "../../infrastructure/dto/json-calls-text";
import { fakeBench } from "../../testing/fake-bench";
import type { CaseSpec } from "../../testing/fixtures";
import { ScorePresenter } from "./score-presenter";

const NOW = new Date(2026, 10, 26, 12, 0);
const at = (daysAgo: number, h: number): string => new Date(2026, 10, 26 - daysAgo, h, 0).toISOString();
const c = (id: string, v: number): CaseSpec => ({ id, name: id, tool: v, outcome: v, trajectory: v });
const tick = () => new Promise((r) => setTimeout(r, 0));

/** The presenter on the fake bench; `around` can hold or change an answer before it reaches the presenter. */
function setup(around?: (fetch: FetchLike) => FetchLike) {
  const bench = fakeBench();
  const feed = memoryFeed();
  const presenter = new ScorePresenter({ source: httpScoreSource(apiClient(around ? around(bench.fetch) : bench.fetch)), feed: httpScoreFeed(feed), calls: jsonCallsText, projectId: "p1", projectName: "pessoal", now: () => NOW });
  const emit = (change: Record<string, unknown>) => feed.emit("/events?project_id=p1", { bench_run: change });
  return { bench, presenter, emit, vm: () => presenter.viewModel };
}

describe("Score edge cases", () => {
  test("Replay of a case deleted elsewhere shows the refusal naming it, and nothing is left running", async () => {
    const { bench, presenter, vm } = setup();
    bench.cases.push({ id: "a", name: "a", prompt: "a", tools: [{ name: "t" }] });
    await presenter.start();
    bench.cases.length = 0; // deleted from another tab or the CLI
    await presenter.replayCase("a");
    assert.equal(vm().running, null);
    assert.match(vm().problemTitle, /did not start/);
    assert.match(vm().problem, /a/);
    // The person can try again: the page is not stuck.
    bench.cases.push({ id: "b", name: "b", prompt: "b", tools: [{ name: "t" }] });
    await presenter.replayCase("b");
    assert.equal(vm().problem, "");
    assert.equal(vm().running?.total, 1);
  });

  test("Run bench with no cases is refused by the harness, shown, and starts nothing", async () => {
    const { bench, presenter, vm } = setup();
    bench.refuseRun("the project has no bench cases");
    await presenter.start();
    assert.equal(vm().state, "no-cases");
    await presenter.runBench();
    assert.equal(vm().running, null);
    assert.match(vm().problem, /no bench cases/);
    assert.deepEqual(bench.runs, []);
  });

  test("a baseline that is not one of this project's runs is ignored without asking the harness", async () => {
    const { bench, presenter, vm } = setup();
    bench.cases.push({ id: "a", name: "a", prompt: "a", tools: [{ name: "t" }] });
    bench.scorecards.push({ id: "s1", at: at(1, 9), cases: [c("a", 0.8)] }, { id: "s2", at: at(0, 9), cases: [c("a", 0.9)] });
    await presenter.start();
    const before = vm().baselineName;
    const asked = bench.requests.length;
    await presenter.selectBaseline("foreign-project-run");
    assert.equal(vm().baselineName, before);
    assert.equal(bench.requests.length, asked);
    assert.equal(vm().problem, "");
  });

  test("a harness that refuses the baseline (another project's run) leaves the page readable", async () => {
    const { bench, presenter, vm } = setup((fetch) => async (input, init) => {
      if (input.includes("/compare") && input.includes("baseline=s1")) return new Response(JSON.stringify({ error: "baseline belongs to another project", code: "BENCH_BASELINE_OTHER_PROJECT" }), { status: 422, headers: { "Content-Type": "application/json" } });
      return fetch(input, init);
    });
    bench.cases.push({ id: "a", name: "a", prompt: "a", tools: [{ name: "t" }] });
    bench.scorecards.push({ id: "s1", at: at(2, 9), cases: [c("a", 0.8)] }, { id: "s2", at: at(1, 9), cases: [c("a", 0.7)] }, { id: "s3", at: at(0, 9), cases: [c("a", 0.9)] });
    await presenter.start();
    await presenter.selectBaseline("s1");
    assert.equal(vm().status, "ready");
    assert.match(vm().problem, /comparison could not be read/);
    assert.ok(vm().rows.length > 0, "the case rows stay");
  });

  test("a feed event for our run while Cancel is in flight keeps it cancelling; the cancelled event then ends it", async () => {
    let release: () => void = () => {};
    const gate = new Promise<void>((r) => (release = r));
    const { bench, presenter, emit, vm } = setup((fetch) => async (input, init) => {
      if (input.endsWith("/cancel")) await gate;
      return fetch(input, init);
    });
    bench.cases.push({ id: "a", name: "a", prompt: "a", tools: [{ name: "t" }] }, { id: "b", name: "b", prompt: "b", tools: [{ name: "t" }] });
    await presenter.start();
    await presenter.runBench();
    const cancelling = presenter.cancelRun();
    await tick();
    assert.equal(vm().running?.cancelling, true);
    emit({ run_id: "run1", status: "running", done: 1, total: 2 });
    await tick();
    assert.equal(vm().running?.done, 1);
    assert.equal(vm().running?.cancelling, true, "progress must not undo the Cancel the person asked for");
    release();
    await cancelling;
    emit({ run_id: "run1", status: "cancelled", done: 1, total: 2 });
    await tick();
    assert.equal(vm().running, null);
    assert.equal(vm().failedRun, null);
  });

  test("a progress event arriving after the run ended does not bring the banner back", async () => {
    const { bench, presenter, emit, vm } = setup();
    bench.cases.push({ id: "a", name: "a", prompt: "a", tools: [{ name: "t" }] });
    await presenter.start();
    await presenter.runBench();
    emit({ run_id: "run1", status: "cancelled", done: 0, total: 1 });
    await tick();
    assert.equal(vm().running, null);
    emit({ run_id: "run1", status: "running", done: 1, total: 1 });
    await tick();
    assert.equal(vm().running, null, "a late running event for a run that already ended is stale");
  });
});
