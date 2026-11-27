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

function setup() {
  const bench = fakeBench();
  const presenter = new ScorePresenter({ source: httpScoreSource(apiClient(bench.fetch)), feed: httpScoreFeed(memoryFeed()), calls: jsonCallsText, projectId: "p1", projectName: "pessoal", now: () => NOW });
  bench.cases.push({ id: "a", name: "reads the readme", prompt: "Read the README.", tools: [{ name: "read_file", result: "# Demo" }], expect: { tools: [{ tool: "read_file", params: { path: "README.md" } }], ordered: true, reference: "A demo." } });
  const card = (spec: CardSpec) => bench.scorecards.push(spec);
  return { bench, presenter, card, vm: () => presenter.viewModel };
}

describe("the Cases view", () => {
  test("a case row carries its pattern, mock tools, a five-run trend and its score in the run read", async () => {
    const { presenter, card, vm } = setup();
    for (let i = 0; i < 7; i++) card({ id: `s${i}`, at: at(7 - i, 10), cases: [{ id: "a", name: "reads the readme", tool: i / 10 }] });
    await presenter.start();
    const row = vm().cases[0]!;
    assert.equal(row.title, "reads the readme");
    assert.equal(row.tools, "1 tool · 1 canned result");
    assert.equal(row.score, "0.60");
    assert.deepEqual(row.trend, [0.2, 0.3, 0.4, 0.5, 0.6]);
  });

  test("a case no run scored has a dash and an empty trend", async () => {
    const { presenter, vm } = setup();
    await presenter.start();
    assert.equal(vm().cases[0]?.score, "—");
    assert.deepEqual(vm().cases[0]?.trend, []);
  });

  test("a case opens in place to its trace: each call in mono with the judge's word, red only where it failed", async () => {
    const { presenter, card, vm } = setup();
    card({ id: "s1", at: at(1, 10), cases: [{ id: "a", tool: 0.9, trajectory: 0.9 }] });
    card({
      id: "s2",
      at: at(0, 10),
      cases: [{ id: "a", tool: 0.5, trajectory: 0.5, calls: [{ tool: "write_file", params: { path: "x" } }, { tool: "read_file", params: { path: "README.md" } }], steps: [{ step: 1, ok: false, note: "early: read first" }, { step: 2, ok: true, note: "expected" }, { step: 3, ok: false, note: "missing step: say done" }], reasons: ["It wrote before it read."], finalText: "Done." }],
    });
    await presenter.start();
    presenter.openCase("a");
    const d = vm().cases[0]?.trace;
    assert.ok(d);
    assert.deepEqual(d.steps.map((s) => ({ call: s.call, note: s.note, failed: s.failed })), [
      { call: 'write_file {"path":"x"}', note: "early: read first", failed: true },
      { call: 'read_file {"path":"README.md"}', note: "expected", failed: false },
      { call: "", note: "missing step: say done", failed: true },
    ]);
    assert.equal(d.judge, "It wrote before it read.");
    assert.equal(d.now, "0.50");
    assert.equal(d.baseline, "0.90");
    assert.equal(d.prompt, "Read the README.");
    presenter.openCase("a");
    assert.equal(vm().cases[0]?.trace, null);
  });

  test("a case in a run that did not score it has no trace to show, and says so", async () => {
    const { presenter, vm } = setup();
    await presenter.start();
    presenter.openCase("a");
    assert.equal(vm().cases[0]?.trace?.steps.length, 0);
    assert.match(vm().cases[0]?.trace?.judge ?? "", /No run has scored this case/);
  });
});

describe("Edit expectation", () => {
  test("the form starts from the case's expectation and Save sends it back as edited", async () => {
    const { presenter, bench, vm } = setup();
    await presenter.start();
    presenter.openCase("a");
    presenter.startEdit("a");
    const form = vm().form!;
    assert.equal(form.mode, "edit");
    assert.equal(form.calls, 'read_file {"path":"README.md"}');
    assert.equal(form.ordered, true);
    assert.equal(form.reference, "A demo.");
    presenter.setForm({ calls: "read_file", ordered: false, reference: "New reference." });
    await presenter.saveForm();
    assert.equal(vm().form, null);
    assert.deepEqual(bench.cases[0]?.expect, { tools: [{ tool: "read_file" }], ordered: false, reference: "New reference." });
    assert.deepEqual(vm().cases[0]?.trace?.expectation.calls, "read_file");
  });

  test("a line that is not a JSON object is refused on its line before anything is sent", async () => {
    const { presenter, bench, vm } = setup();
    await presenter.start();
    presenter.startEdit("a");
    presenter.setForm({ calls: 'read_file\nlist {"a":' });
    await presenter.saveForm();
    assert.match(vm().form?.errors.calls ?? "", /Line 2/);
    assert.equal(bench.requests.some((r) => r.startsWith("PATCH")), false);
  });

  test("a refusal from the harness lands on the form with its code and keeps what was typed", async () => {
    const { presenter, vm } = setup();
    await presenter.start();
    presenter.startEdit("a");
    presenter.setForm({ calls: "write_file" });
    await presenter.saveForm();
    assert.equal(vm().form?.calls, "write_file");
    assert.match(vm().form?.errors.calls ?? "", /UNKNOWN_TOOL.*write_file has no mock/);
    presenter.cancelForm();
    assert.equal(vm().form, null);
  });
});

describe("Add case and the recorded trajectories", () => {
  test("a new case is written as text and listed once saved", async () => {
    const { presenter, bench, vm } = setup();
    await presenter.start();
    presenter.startAdd();
    presenter.setForm({ name: "lists files", prompt: "List the files.", mocks: "list_dir = a.txt\nwrite_file ! denied", calls: 'list_dir {"path": "."}', ordered: false, reference: "" });
    await presenter.saveForm();
    assert.equal(vm().form, null);
    assert.equal(vm().caseCount, 2);
    assert.deepEqual(bench.cases[1]?.tools.map((t) => [t.name, t.is_error ?? false]), [["list_dir", false], ["write_file", true]]);
  });

  test("a refused new case names the prompt and the expectation fields", async () => {
    const { presenter, vm } = setup();
    await presenter.start();
    presenter.startAdd();
    await presenter.saveForm();
    assert.match(vm().form?.errors.prompt ?? "", /REQUIRED/);
    assert.match(vm().form?.errors.calls ?? "", /NO_EXPECTATION/);
  });

  test("the recorded trajectories are offered, and choosing one makes a case from it", async () => {
    const { presenter, bench, vm } = setup();
    bench.trajectories.push({ id: "t1", task_id: "task1", prompt: "Fix the failing test", steps: [{}, {}, {}], recorded_at: at(1, 9) });
    await presenter.start();
    await presenter.openTrajectories();
    assert.deepEqual(vm().trajectories?.items.map((t) => [t.id, t.prompt, t.steps]), [["t1", "Fix the failing test", "3 steps"]]);
    await presenter.chooseTrajectory("t1");
    assert.equal(vm().trajectories, null);
    assert.equal(vm().caseCount, 2);
    assert.equal(vm().cases[1]?.source, "recorded");
  });

  test("with no recorded trajectory the list says how to get one", async () => {
    const { presenter, vm } = setup();
    await presenter.start();
    await presenter.openTrajectories();
    assert.equal(vm().trajectories?.items.length, 0);
    assert.match(vm().trajectories?.empty ?? "", /Trajectory recording/);
  });
});

describe("Delete", () => {
  test("it asks once, then removes the case", async () => {
    const { presenter, bench, vm } = setup();
    await presenter.start();
    presenter.requestDelete("a");
    assert.equal(vm().cases[0]?.confirmingDelete, true);
    assert.equal(bench.requests.some((r) => r.startsWith("DELETE")), false);
    await presenter.confirmDelete("a");
    assert.equal(vm().caseCount, 0);
    assert.equal(bench.cases.length, 0);
  });

  test("backing out keeps the case", async () => {
    const { presenter, vm } = setup();
    await presenter.start();
    presenter.requestDelete("a");
    presenter.cancelDelete();
    assert.equal(vm().cases[0]?.confirmingDelete, false);
    assert.equal(vm().caseCount, 1);
  });
});
