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
/** What the fake bench says a run started at (UTC). */
const RUN_STARTED = "2026-11-26T12:00:00Z";
/** Local time on Nov 26 minus `daysAgo`, as the API writes it. */
const at = (daysAgo: number, h: number, m = 0): string => new Date(2026, 10, 26 - daysAgo, h, m).toISOString();
const c = (id: string, tool?: number, outcome?: number, trajectory?: number): CaseSpec => ({ id, name: id, ...(tool !== undefined ? { tool } : {}), ...(outcome !== undefined ? { outcome } : {}), ...(trajectory !== undefined ? { trajectory } : {}) });

function setup() {
  const bench = fakeBench();
  const feed = memoryFeed();
  const clock = { now: NOW };
  const presenter = new ScorePresenter({ source: httpScoreSource(apiClient(bench.fetch)), feed: httpScoreFeed(feed), calls: jsonCallsText, projectId: "p1", projectName: "pessoal", now: () => clock.now });
  const addCase = (id: string) => bench.cases.push({ id, name: id, prompt: id, tools: [{ name: "t" }] });
  const addCard = (spec: CardSpec) => bench.scorecards.push(spec);
  const emit = (change: Record<string, unknown>) => feed.emit("/events?project_id=p1", { bench_run: change });
  return { bench, feed, presenter, addCase, addCard, emit, clock, vm: () => presenter.viewModel };
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
    await presenter.selectBaseline("s0");
    assert.equal(vm().baselineName, "run 1 · Nov 24 09:00");
    await presenter.selectBaseline("s2");
    assert.equal(vm().baselineName, "run 1 · Nov 24 09:00");
    await presenter.selectBaseline(null);
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
    await presenter.selectRead("s2");
    assert.equal(vm().baselineName, "run 1 · yesterday 18:02");
  });

  test("a run of some of the cases is partial too, and is never the default baseline", async () => {
    const { presenter, vm, addCard } = two();
    addCard({ id: "s3", at: at(0, 11, 50), status: "partial", cases: [c("a", 1, 1, 1)] });
    await presenter.start();
    assert.equal(vm().baselineName, "run 2 · today 11:40");
    assert.equal(vm().runs.find((r) => r.id === "s3")?.partial, true);
  });

  test("a cancelled run and a run of some cases are both partial, and the Runs view keeps them apart", async () => {
    const { presenter, vm, addCard } = two();
    addCard({ id: "s3", at: at(0, 11, 50), status: "cancelled", cases: [c("a", 1, 1, 1)] });
    addCard({ id: "s4", at: at(0, 11, 55), status: "partial", cases: [c("a", 1, 1, 1)] });
    await presenter.start();
    const row = (id: string) => vm().runs.find((r) => r.id === id)!;
    assert.deepEqual([row("s3").partial, row("s4").partial, row("s2").partial], [true, true, false]);
    assert.deepEqual([row("s3").kind, row("s4").kind, row("s2").kind], ["cancelled", "cases", "finished"]);
    // Neither is a baseline: the newest finished run still is.
    assert.equal(vm().baselineName, "run 2 · today 11:40");
  });

  test("the deltas are the harness's: the page asks for the comparison with the chosen baseline", async () => {
    const { presenter, bench, addCard } = two();
    addCard({ id: "s0", at: at(2, 9), cases: [c("a", 1, 1, 1)] });
    await presenter.start();
    await presenter.selectBaseline("s0");
    assert.ok(bench.requests.filter((r) => r.endsWith("/compare")).length >= 2);
  });

  test("Replay this case runs that one case and shows it running like Run bench", async () => {
    const { presenter, vm, bench } = two();
    await presenter.start();
    bench.cases.push({ id: "k1", prompt: "p", tools: [] });
    await presenter.start();
    await presenter.replayCase("k1");
    assert.deepEqual([vm().running?.done, vm().running?.total], [0, 1]);
    assert.equal(bench.requests.filter((r) => r === "POST /projects/p1/bench/runs").length, 1);
  });

  test("reading a run from the list opens the Scorecard on it", async () => {
    const { presenter, vm } = two();
    await presenter.start();
    presenter.setView("runs");
    assert.equal(vm().view, "runs");
    await presenter.selectRead("s1");
    assert.equal(vm().view, "scorecard");
    assert.equal(vm().readName, "run 1 · yesterday 18:02");
    // Later runs exist, so the menu stays: this is not a first run.
    assert.equal(vm().state, "no-baseline");
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
    assert.deepEqual(vm().cases.map((c) => [c.id, c.title, c.tools, c.source]), [["k", "brief", "2 tools · 2 canned results", "written"]]);
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
    assert.deepEqual([vm().running?.done, vm().running?.total], [0, 1]);
    await presenter.runBench();
    assert.equal(bench.requests.filter((r) => r.startsWith("POST")).length, 1);
    emit({ run_id: "run1", status: "running", done: 1, total: 1 });
    assert.deepEqual([vm().running?.done, vm().running?.total], [1, 1]);
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
    assert.deepEqual([vm().running?.done, vm().running?.total], [2, 9]);
  });

  test("it stops following the feed when it stops", async () => {
    const { presenter, feed } = setup();
    await presenter.start();
    assert.equal(feed.followers("/events?project_id=p1"), 1);
    presenter.stop();
    assert.equal(feed.followers("/events?project_id=p1"), 0);
  });
});

describe("A run in progress", () => {
  const atRun = (clock: { now: Date }, seconds: number) => {
    clock.now = new Date(new Date(RUN_STARTED).getTime() + seconds * 1000);
  };

  test("the banner counts the cases done, the share of the bar and the time since the run started", async () => {
    const { presenter, vm, addCase, addCard, emit, clock } = setup();
    for (const id of ["a", "b", "c", "d"]) addCase(id);
    addCard({ id: "s1", at: at(1, 9), cases: [c("a", 0.8, 0.8, 0.8)] });
    await presenter.start();
    atRun(clock, 0);
    await presenter.runBench();
    assert.deepEqual([vm().running?.runId, vm().running?.done, vm().running?.total, vm().running?.percent, vm().running?.elapsed], ["run1", 0, 4, 0, "0s"]);
    emit({ run_id: "run1", status: "running", done: 1, total: 4 });
    atRun(clock, 160);
    presenter.tick();
    assert.deepEqual([vm().running?.done, vm().running?.percent, vm().running?.elapsed], [1, 25, "2m 40s"]);
  });

  test("the scorecard keeps showing the last finished comparison while the run goes", async () => {
    const { presenter, vm, addCase, addCard, emit } = setup();
    addCase("a");
    addCard({ id: "s1", at: at(2, 9), cases: [c("a", 0.8, 0.8, 0.8)] });
    addCard({ id: "s2", at: at(1, 9), cases: [c("a", 0.6, 0.6, 0.6)] });
    await presenter.start();
    const before = vm().readName;
    await presenter.runBench();
    emit({ run_id: "run1", status: "running", done: 0, total: 1 });
    assert.equal(vm().readName, before);
    assert.equal(vm().state, "compared");
  });

  test("a run another session started learns its start from the harness", async () => {
    const { presenter, vm, bench, emit, clock } = setup();
    await presenter.start();
    bench.runs.push({ id: "elsewhere", status: "running", total: 9, done: 2, started_at: RUN_STARTED });
    atRun(clock, 75);
    emit({ run_id: "elsewhere", status: "running", done: 2, total: 9 });
    await tick();
    assert.deepEqual([vm().running?.runId, vm().running?.elapsed], ["elsewhere", "1m 15s"]);
  });

  test("Cancel asks the harness to stop the run and says so until the feed ends it; the cancelled run is listed as partial", async () => {
    const { presenter, vm, addCase, addCard, bench, emit } = setup();
    addCase("a");
    addCase("b");
    addCard({ id: "s1", at: at(1, 9), cases: [c("a", 0.8, 0.8, 0.8), c("b", 0.8, 0.8, 0.8)] });
    await presenter.start();
    await presenter.runBench();
    await presenter.cancelRun();
    assert.ok(bench.requests.includes("POST /projects/p1/bench/runs/run1/cancel"));
    assert.equal(vm().running?.cancelling, true);
    addCard({ id: "run1", at: at(0, 11, 59), status: "cancelled", cases: [c("a", 0.4, 0.4, 0.4)] });
    emit({ run_id: "run1", status: "cancelled", done: 1, total: 2, scorecard_id: "run1" });
    await tick();
    assert.equal(vm().running, null);
    assert.equal(vm().failedRun, null);
    presenter.setView("runs");
    assert.match(vm().runs[0]?.status ?? "", /^partial · 1 of 2/);
  });

  test("a refused Cancel is shown and the run stays running", async () => {
    const { presenter, vm, addCase, bench } = setup();
    addCase("a");
    await presenter.start();
    await presenter.runBench();
    bench.runs[0]!.status = "finished";
    await presenter.cancelRun();
    assert.match(vm().problem, /finished/);
    assert.equal(vm().running?.cancelling, false);
  });

  test("a failed run shows the danger banner with why, keeps what was scored as a partial run, and Retry starts a new run", async () => {
    const { presenter, vm, addCase, addCard, bench, emit } = setup();
    addCase("a");
    addCase("b");
    await presenter.start();
    await presenter.runBench();
    bench.runs[0]!.status = "failed";
    bench.runs[0]!.error = "the claude CLI exited 1";
    addCard({ id: "run1", at: at(0, 11, 59), status: "cancelled", cases: [c("a", 0.4, 0.4, 0.4)] });
    emit({ run_id: "run1", status: "failed", done: 1, total: 2, scorecard_id: "run1" });
    await tick();
    assert.equal(vm().running, null);
    assert.deepEqual([vm().failedRun?.title, vm().failedRun?.message, vm().failedRun?.done, vm().failedRun?.total], ["Run 1 failed", "the claude CLI exited 1", 1, 2]);
    await presenter.retryRun();
    assert.equal(vm().failedRun, null);
    assert.equal(vm().running?.runId, "run2");
  });

  test("a failed run's banner can be dismissed", async () => {
    const { presenter, vm, addCase, bench, emit } = setup();
    addCase("a");
    await presenter.start();
    await presenter.runBench();
    bench.runs[0]!.status = "failed";
    bench.runs[0]!.error = "boom";
    emit({ run_id: "run1", status: "failed", done: 0, total: 1 });
    await tick();
    assert.ok(vm().failedRun);
    presenter.dismissFailure();
    assert.equal(vm().failedRun, null);
  });
});
