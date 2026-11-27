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

function setup(around?: (fetch: FetchLike) => FetchLike) {
  const bench = fakeBench();
  const presenter = new ScorePresenter({ source: httpScoreSource(apiClient(around ? around(bench.fetch) : bench.fetch)), feed: httpScoreFeed(memoryFeed()), calls: jsonCallsText, projectId: "p1", projectName: "pessoal", now: () => NOW });
  bench.cases.push({ id: "a", name: "a", prompt: "a", tools: [{ name: "t" }] });
  return { bench, presenter, vm: () => presenter.viewModel };
}

describe("the baseline menu", () => {
  test("opening the oldest run, with later runs in the project, still offers the menu: it is not a first run", async () => {
    const { bench, presenter, vm } = setup();
    bench.scorecards.push({ id: "s1", at: at(2, 9), cases: [c("a", 0.7)] }, { id: "s2", at: at(1, 9), cases: [c("a", 0.8)] });
    await presenter.start();
    await presenter.selectRead("s1");
    assert.notEqual(vm().state, "first-run");
    assert.equal(vm().state, "no-baseline");
    // The person picks a later run as the baseline and the page compares with it.
    await presenter.selectBaseline("s2");
    assert.equal(vm().state, "compared");
    assert.match(vm().baselineName, /run 2/);
  });

  test("when the only earlier runs are cancelled or partial there is no default baseline, but the menu offers them", async () => {
    const { bench, presenter, vm } = setup();
    bench.scorecards.push({ id: "s1", at: at(2, 9), status: "cancelled", cases: [c("a", 0.7)] }, { id: "s2", at: at(0, 9), cases: [c("a", 0.8)] });
    await presenter.start();
    assert.equal(vm().state, "no-baseline");
    await presenter.selectBaseline("s1");
    assert.equal(vm().state, "compared");
  });

  test("first run reads only when the project has no other run at all", async () => {
    const { bench, presenter, vm } = setup();
    bench.scorecards.push({ id: "s1", at: at(0, 9), cases: [c("a", 0.8)] });
    await presenter.start();
    assert.equal(vm().state, "first-run");
  });

  test("a failed compare request is an error with Retry, never a first run", async () => {
    let failing = true;
    const { bench, presenter, vm } = setup((fetch) => async (input, init) => {
      if (failing && input.includes("/compare")) return new Response(JSON.stringify({ error: "boom", code: "INTERNAL" }), { status: 500, headers: { "Content-Type": "application/json" } });
      return fetch(input, init);
    });
    bench.scorecards.push({ id: "s1", at: at(1, 9), cases: [c("a", 0.7)] }, { id: "s2", at: at(0, 9), cases: [c("a", 0.8)] });
    await presenter.start();
    assert.notEqual(vm().state, "first-run");
    assert.equal(vm().compareFailed, true);
    assert.match(vm().problem, /comparison could not be read/);
    failing = false;
    await presenter.retryCompare();
    assert.equal(vm().compareFailed, false);
    assert.equal(vm().problem, "");
    assert.equal(vm().state, "compared");
  });

  test("a later good compare clears the problem a failed one left", async () => {
    let failing = true;
    const { bench, presenter, vm } = setup((fetch) => async (input, init) => {
      if (failing && input.includes("/compare")) return new Response(JSON.stringify({ error: "boom", code: "INTERNAL" }), { status: 500, headers: { "Content-Type": "application/json" } });
      return fetch(input, init);
    });
    bench.scorecards.push({ id: "s1", at: at(1, 9), cases: [c("a", 0.7)] }, { id: "s2", at: at(0, 9), cases: [c("a", 0.8)] });
    await presenter.start();
    failing = false;
    await presenter.selectBaseline("s1");
    assert.equal(vm().problem, "");
    assert.equal(vm().compareFailed, false);
  });
});

describe("a tile with no group answer", () => {
  test("reads as a dash, not '= run N'", async () => {
    const { bench, presenter, vm } = setup((fetch) => async (input, init) => {
      const res = await fetch(input, init);
      if (!input.includes("/compare")) return res;
      const body = (await res.json()) as Record<string, unknown>;
      delete body["groups"];
      return new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": "application/json" } });
    });
    bench.scorecards.push({ id: "s1", at: at(1, 9), cases: [c("a", 0.7)] }, { id: "s2", at: at(0, 9), cases: [c("a", 0.8)] });
    await presenter.start();
    const tile = vm().tiles.find((t) => t.key === "tool_use");
    assert.ok(tile);
    assert.doesNotMatch(tile.note, /= run/);
    assert.equal(tile.note, "—");
  });
});
