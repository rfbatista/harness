import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { apiClient } from "../../../../shared/infrastructure/api-client";
import { memoryFeed } from "../../../../shared/infrastructure/memory-feed";
import { httpScoreFeed } from "../../infrastructure/http-score-feed";
import { httpScoreSource } from "../../infrastructure/http-score-source";
import { jsonCallsText } from "../../infrastructure/dto/json-calls-text";
import { fakeBench } from "../../testing/fake-bench";
import type { CardSpec } from "../../testing/fixtures";
import { ScorePresenter } from "./score-presenter";

const NOW = new Date(2026, 10, 26, 12, 0);
const at = (daysAgo: number, h: number): string => new Date(2026, 10, 26 - daysAgo, h).toISOString();

function setup(cards: CardSpec[]) {
  const bench = fakeBench();
  bench.cases.push({ id: "a", name: "a", prompt: "a", tools: [{ name: "t" }] });
  for (const card of cards) bench.scorecards.push(card);
  const presenter = new ScorePresenter({ source: httpScoreSource(apiClient(bench.fetch)), feed: httpScoreFeed(memoryFeed()), calls: jsonCallsText, projectId: "p1", projectName: "pessoal", now: () => NOW });
  return { presenter, vm: () => presenter.viewModel };
}
const cases = [{ id: "a", name: "a", tool: 1, outcome: 1, trajectory: 1 }];
const base = { agents: 1, skills: 2, instructions: 3 };

describe("the setup line under the compare bar", () => {
  test("counts what the run ran on and names the parts that changed since the baseline", async () => {
    const { presenter, vm } = setup([
      { id: "s1", at: at(1, 9), fingerprint: "f1", ...base, parts: { agent: "h1", model: "m1", "instruction AGENTS.md": "i1", "skill skills/verify-harness/SKILL.md": "k1" }, cases },
      { id: "s2", at: at(0, 9), fingerprint: "f2", ...base, parts: { agent: "h1", model: "m1", "instruction AGENTS.md": "i2", "skill skills/verify-harness/SKILL.md": "k2" }, cases },
    ]);
    await presenter.start();
    assert.equal(vm().fingerprint?.parts, "agents 1 · skills 2 · instructions 3 · model opus-5-5 · claude 2.1.14");
    assert.equal(vm().fingerprint?.changed, "AGENTS.md, skill verify-harness");
  });

  test("a setup recorded before parts existed says so, and a changed fingerprint falls back to the named fields", async () => {
    const { presenter, vm } = setup([
      { id: "s1", at: at(1, 9), fingerprint: "f1", cases },
      { id: "s2", at: at(0, 9), fingerprint: "f2", model: "sonnet-5-5", cases },
    ]);
    await presenter.start();
    assert.equal(vm().fingerprint?.parts, "agents, skills and instructions not recorded · model sonnet-5-5 · claude 2.1.14");
    assert.equal(vm().fingerprint?.changed, "model");
  });

  test("a baseline without parts against a run with parts cannot name the parts, and says what it can", async () => {
    const { presenter, vm } = setup([
      { id: "s1", at: at(1, 9), fingerprint: "f1", cases },
      { id: "s2", at: at(0, 9), fingerprint: "f2", ...base, parts: { agent: "h1" }, cases },
    ]);
    await presenter.start();
    assert.equal(vm().fingerprint?.parts, "agents 1 · skills 2 · instructions 3 · model opus-5-5 · claude 2.1.14");
    assert.equal(vm().fingerprint?.changed, "agents, skills or instructions");
  });

  test("an unchanged setup names nothing", async () => {
    const parts = { agent: "h1", model: "m1" };
    const { presenter, vm } = setup([
      { id: "s1", at: at(1, 9), fingerprint: "f1", ...base, parts, cases },
      { id: "s2", at: at(0, 9), fingerprint: "f1", ...base, parts, cases },
    ]);
    await presenter.start();
    assert.equal(vm().fingerprint?.changed, "");
  });
});
