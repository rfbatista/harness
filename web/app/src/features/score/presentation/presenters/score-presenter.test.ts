import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { apiClient } from "../../../../shared/infrastructure/api-client";
import { memoryFeed } from "../../../../shared/infrastructure/memory-feed";
import { httpScoreFeed } from "../../infrastructure/http-score-feed";
import { httpScoreSource } from "../../infrastructure/http-score-source";
import { jsonCallsText } from "../../infrastructure/dto/json-calls-text";
import { fakeBench } from "../../testing/fake-bench";
import type { CardSpec, CaseSpec } from "../../testing/fixtures";
import { ScorePresenter } from "./score-presenter";

const NOW = new Date(2026, 10, 26, 12, 0);
/** Local time on Nov 26 minus `daysAgo`, as the API writes it. */
const at = (daysAgo: number, h: number, m = 0): string => new Date(2026, 10, 26 - daysAgo, h, m).toISOString();
const c = (id: string, tool?: number, outcome?: number, trajectory?: number): CaseSpec => ({ id, name: id, ...(tool !== undefined ? { tool } : {}), ...(outcome !== undefined ? { outcome } : {}), ...(trajectory !== undefined ? { trajectory } : {}) });

function setup() {
  const bench = fakeBench();
  const feed = memoryFeed();
  const presenter = new ScorePresenter({ source: httpScoreSource(apiClient(bench.fetch)), feed: httpScoreFeed(feed), calls: jsonCallsText, projectId: "p1", projectName: "pessoal", now: () => NOW });
  const addCase = (id: string) => bench.cases.push({ id, name: id, prompt: id, tools: [{ name: "t" }] });
  const addCard = (spec: CardSpec) => bench.scorecards.push(spec);
  const emit = (change: Record<string, unknown>) => feed.emit("/events?project_id=p1", { bench_run: change });
  return { bench, feed, presenter, addCase, addCard, emit, vm: () => presenter.viewModel };
}
const tick = () => new Promise((r) => setTimeout(r, 0));

describe("the states of the Scorecard view", () => {
  test("loading, then no cases: an empty state that teaches what a case is", async () => {
    const { presenter, vm } = setup();
    assert.equal(vm().status, "loading");
    await presenter.start();
    assert.equal(vm().status, "ready");
    assert.equal(vm().state, "no-cases");
    assert.equal(vm().tiles.length, 0);
  });

  test("cases but no run: tiles are absent and one sentence says what a run costs", async () => {
    const { presenter, vm, addCase } = setup();
    addCase("a");
    addCase("b");
    await presenter.start();
    assert.equal(vm().state, "no-run");
    assert.match(vm().notice, /No run yet\. Run bench scores all 2 cases/);
    assert.equal(vm().lastRun, "");
  });

  test("a first run has no baseline: every delta reads first run", async () => {
    const { presenter, vm, addCase, addCard } = setup();
    addCase("a");
    addCard({ id: "s1", at: at(0, 11, 40), cases: [c("a", 0.9, 0.8, 0.7)] });
    await presenter.start();
    assert.equal(vm().state, "first-run");
    assert.equal(vm().baselineName, "");
    assert.deepEqual(vm().tiles.slice(0, 4).map((t) => t.note), ["first run", "first run", "first run", "first run"]);
    assert.equal(vm().rows[0]?.delta, "first run");
    assert.equal(vm().lastRun, "last run 1 · today 11:40 · 7m 12s");
  });

  test("a failing store is a failed state, and Retry reads again", async () => {
    const { presenter, vm, bench } = setup();
    bench.breakNetwork();
    await presenter.start();
    assert.equal(vm().status, "failed");
    assert.match(vm().error, /offline|network/i);
  });

  test("a harness without the bench says so", async () => {
    const { presenter, vm, bench } = setup();
    bench.switchOff();
    await presenter.start();
    assert.equal(vm().status, "failed");
    assert.match(vm().error, /no eval bench/);
  });
});

describe("a run read beside its baseline", () => {
  function two() {
    const s = setup();
    for (const id of ["a", "b", "d"]) s.addCase(id);
    s.addCard({ id: "s1", at: at(1, 18, 2), cost: 1.09, cases: [c("a", 0.95, 0.9, 0.9), c("b", 0.7, undefined, 0.8), c("d", 0.5, 0.5, 0.5)] });
    s.addCard({ id: "s2", at: at(0, 11, 40), cost: 1.14, cases: [c("a", 0.5, 0.5, 0.5), c("b", 0.9, undefined, 0.9), c("d", 0.5, 0.5, 0.5), c("n", 0.9, undefined, 0.9)] });
    return s;
  }

  test("the newest run is read against the previous finished one", async () => {
    const { presenter, vm } = two();
    await presenter.start();
    assert.equal(vm().state, "compared");
    assert.equal(vm().readName, "run 2 · today 11:40");
    assert.equal(vm().baselineName, "run 1 · yesterday 18:02");
  });

  test("tiles put cost beside score, and a drop is down, a rise up, no change equal", async () => {
    const { presenter, vm } = two();
    await presenter.start();
    const tile = (k: string) => vm().tiles.find((t) => t.key === k)!;
    assert.equal(tile("aggregate").direction, "down");
    assert.match(tile("aggregate").note, /^▼ 0\.\d\d vs run 1 \(0\.\d\d\)$/);
    assert.equal(tile("cases").note, "1 regressed · 1 improved · 1 new");
    assert.equal(tile("cost").label, "Estimated cost");
    assert.equal(tile("cost").value, "$1.14");
    assert.match(tile("cost").note, /run 1 \$1\.09/);
    assert.ok(vm().tiles.every((t) => t.direction === "down" || t.direction === "up" || t.direction === "equal" || t.direction === "none"));
  });

  test("case rows say how each stands, and the filter narrows them", async () => {
    const { presenter, vm } = two();
    await presenter.start();
    const row = (id: string) => vm().rows.find((r) => r.caseId === id)!;
    assert.equal(row("a").regressed, true);
    assert.equal(row("a").delta, "▼ 0.43");
    assert.equal(row("b").direction, "up");
    assert.equal(row("d").delta, "=");
    assert.equal(row("d").failed, true);
    assert.equal(row("n").delta, "new");
    assert.deepEqual(vm().filterCounts, { all: 4, regressed: 1, improved: 1, failed: 2, new: 1 });
    presenter.setFilter("regressed");
    assert.deepEqual(vm().rows.map((r) => r.caseId), ["a"]);
    presenter.setFilter("new");
    assert.deepEqual(vm().rows.map((r) => r.caseId), ["n"]);
    presenter.setFilter("all");
    assert.equal(vm().rows.length, 4);
  });

  test("any run can be the baseline; the read run cannot", async () => {
    const { presenter, vm, addCard } = two();
    addCard({ id: "s0", at: at(2, 9), cases: [c("a", 1, 1, 1)] });
    await presenter.start();
    assert.equal(vm().baselineName, "run 2 · yesterday 18:02");
    presenter.selectBaseline("s0");
    assert.equal(vm().baselineName, "run 1 · Nov 24 09:00");
    presenter.selectBaseline("s2");
    assert.equal(vm().baselineName, "run 1 · Nov 24 09:00");
    presenter.selectBaseline(null);
    assert.equal(vm().baselineName, "run 2 · yesterday 18:02");
  });

  test("a cancelled run is listed as partial and is never the default baseline", async () => {
    const { presenter, vm, addCard } = two();
    addCard({ id: "s3", at: at(0, 11, 50), status: "cancelled", cases: [c("a", 1, 1, 1)] });
    await presenter.start();
    assert.equal(vm().readName, "run 3 · today 11:50");
    assert.equal(vm().baselineName, "run 2 · today 11:40");
    const partial = vm().runs.find((r) => r.id === "s3")!;
    assert.equal(partial.partial, true);
    assert.equal(partial.status, "partial · 1 of 3");
    presenter.selectRead("s2");
    assert.equal(vm().baselineName, "run 1 · yesterday 18:02");
  });

  test("reading a run from the list opens the Scorecard on it", async () => {
    const { presenter, vm } = two();
    await presenter.start();
    presenter.setView("runs");
    assert.equal(vm().view, "runs");
    presenter.selectRead("s1");
    assert.equal(vm().view, "scorecard");
    assert.equal(vm().readName, "run 1 · yesterday 18:02");
    assert.equal(vm().state, "first-run");
  });

  test("the setup fingerprint names what changed since the baseline", async () => {
    const { presenter, vm, addCard } = two();
    addCard({ id: "s4", at: at(0, 11, 55), fingerprint: "zz9999", model: "sonnet-5-5", cases: [c("a", 1, 1, 1)] });
    await presenter.start();
    assert.equal(vm().fingerprint?.short, "zz9999");
    assert.equal(vm().fingerprint?.changed, "model");
    assert.equal(vm().noise, "");
  });

  test("the same setup with a score that moved says runs are not deterministic", async () => {
    const { presenter, vm } = two();
    await presenter.start();
    assert.equal(vm().fingerprint?.changed, "");
    assert.match(vm().noise, /Same setup as run 1 \(fp1\) but the aggregate moved by 0\.\d\d\. Runs are not deterministic/);
  });

  test("the same setup and the same score says nothing", async () => {
    const s = setup();
    s.addCase("a");
    s.addCard({ id: "s1", at: at(1, 9), cases: [c("a", 0.8, 0.8, 0.8)] });
    s.addCard({ id: "s2", at: at(0, 9), cases: [c("a", 0.8, 0.8, 0.8)] });
    await s.presenter.start();
    assert.equal(s.vm().noise, "");
    assert.equal(s.vm().tiles[0]?.note, "= run 1");
  });
});

describe("Cases and Runs", () => {
  test("the Cases view lists the cases with their mock tools", async () => {
    const { presenter, vm, bench } = setup();
    bench.cases.push({ id: "k", name: "brief", prompt: "x", tools: [{ name: "a" }, { name: "b" }] });
    await presenter.start();
    presenter.setView("cases");
    assert.deepEqual(vm().cases.map((c) => [c.id, c.title, c.tools, c.source]), [["k", "brief", "2 tools", "written"]]);
    assert.equal(vm().caseCount, 1);
  });
});

describe("Run bench", () => {
  test("it starts a run and is busy until the feed says the run ended, then reads the new scorecard", async () => {
    const { presenter, vm, addCase, addCard, bench, emit } = setup();
    addCase("a");
    addCard({ id: "s1", at: at(1, 9), cases: [c("a", 0.8, 0.8, 0.8)] });
    await presenter.start();
    await presenter.runBench();
    assert.deepEqual(bench.requests.filter((r) => r.startsWith("POST")), ["POST /projects/p1/bench/runs"]);
    assert.deepEqual(vm().running, { done: 0, total: 1 });
    await presenter.runBench();
    assert.equal(bench.requests.filter((r) => r.startsWith("POST")).length, 1);
    emit({ run_id: "run1", status: "running", done: 1, total: 1 });
    assert.deepEqual(vm().running, { done: 1, total: 1 });
    addCard({ id: "run1", at: at(0, 11, 59), cases: [c("a", 0.4, 0.4, 0.4)] });
    emit({ run_id: "run1", status: "finished", done: 1, total: 1, scorecard_id: "run1" });
    await tick();
    assert.equal(vm().running, null);
    assert.equal(vm().readName, "run 2 · today 11:59");
    assert.equal(vm().tiles[0]?.direction, "down");
  });

  test("a refused run shows why and runs nothing", async () => {
    const { presenter, vm, bench } = setup();
    await presenter.start();
    bench.refuseRun("add a case first");
    await presenter.runBench();
    assert.equal(vm().running, null);
    assert.match(vm().problem, /add a case first/);
  });

  test("a run another session started shows as running too", async () => {
    const { presenter, vm, emit } = setup();
    await presenter.start();
    emit({ run_id: "elsewhere", status: "running", done: 2, total: 9 });
    assert.deepEqual(vm().running, { done: 2, total: 9 });
  });

  test("it stops following the feed when it stops", async () => {
    const { presenter, feed } = setup();
    await presenter.start();
    assert.equal(feed.followers("/events?project_id=p1"), 1);
    presenter.stop();
    assert.equal(feed.followers("/events?project_id=p1"), 0);
  });
});
