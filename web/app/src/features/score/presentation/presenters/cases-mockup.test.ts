import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { apiClient } from "../../../../shared/infrastructure/api-client";
import { memoryFeed } from "../../../../shared/infrastructure/memory-feed";
import { httpScoreFeed } from "../../infrastructure/http-score-feed";
import { httpScoreSource } from "../../infrastructure/http-score-source";
import { jsonCallsText } from "../../infrastructure/dto/json-calls-text";
import { fakeBench } from "../../testing/fake-bench";
import { ScorePresenter } from "./score-presenter";

const NOW = new Date(2026, 10, 26, 12, 0);
const at = (daysAgo: number, h: number): string => new Date(2026, 10, 26 - daysAgo, h).toISOString();

function setup() {
  const bench = fakeBench();
  const presenter = new ScorePresenter({ source: httpScoreSource(apiClient(bench.fetch)), feed: httpScoreFeed(memoryFeed()), calls: jsonCallsText, projectId: "p1", projectName: "pessoal", now: () => NOW });
  bench.cases.push({
    id: "a",
    name: "reads the readme",
    prompt: "Read the README.",
    tools: [{ name: "read_file", result: "# Demo" }, { name: "write_file", result: "ok" }],
    expect: { tools: [{ tool: "read_file" }], ordered: true, reference: "" },
  });
  return { bench, presenter, vm: () => presenter.viewModel };
}

describe("the Cases and Scorecard views match the mockup's wording", () => {
  test("phase-2 patterns are one muted row", async () => {
    const { presenter, vm } = setup();
    await presenter.start();
    assert.equal(vm().later.length, 1);
    assert.match(vm().later[0] ?? "", /phase 2/);
  });

  test("the mock tools read 'N tools · M canned results'", async () => {
    const { presenter, vm } = setup();
    await presenter.start();
    assert.equal(vm().cases[0]?.tools, "2 tools · 2 canned results");
  });

  test("the trace head names the run, its time and cost for the case, and the judge's model", async () => {
    const { bench, presenter, vm } = setup();
    bench.scorecards.push({ id: "s1", at: at(1, 10), cases: [{ id: "a", tool: 0.9 }] });
    bench.scorecards.push({ id: "s2", at: at(0, 10), judge: "opus-5-5", cases: [{ id: "a", tool: 0.6, cost: 0.09, durationMs: 48_000 }] });
    await presenter.start();
    presenter.openCase("a");
    const d = vm().cases[0]?.trace;
    assert.ok(d);
    assert.equal(d.head, "Run 2 · 48s · $0.09 · judge: opus-5-5");
    assert.equal(d.baselineNote, "run 1 was 0.90 on the same case");
  });

  test("with no judge model recorded the head leaves the judge out, and with no baseline there is no note", async () => {
    const { bench, presenter, vm } = setup();
    bench.scorecards.push({ id: "s1", at: at(0, 10), cases: [{ id: "a", tool: 0.6 }] });
    await presenter.start();
    presenter.openCase("a");
    const d = vm().cases[0]?.trace;
    assert.ok(d);
    assert.equal(d.head, "Run 1 · 1s · $0.05");
    assert.equal(d.baselineNote, "");
  });
});
