// The Score page: the scorecard of a run beside a baseline, the cases and the runs, and Run bench.

import { Presenter } from "../../../../shared/kernel/presenter";
import type { ScoreFeed, ScoreSource } from "../../application/ports";
import { ScoreError, type BenchCase, type RunChange } from "../../domain/bench";
import { compare, defaultBaseline, type CaseRow, type Comparison, type Delta, type Standing } from "../../domain/comparison";
import { cost, deltaShort, duration, score, when } from "../../domain/format";
import { GROUP_LABEL, type Scorecard } from "../../domain/scorecard";

export type View = "scorecard" | "cases" | "runs";
export type Filter = "all" | Standing;
export const FILTERS: readonly Filter[] = ["all", "regressed", "improved", "failed", "new"];

/** What the Scorecard view shows: no cases to score, cases with no run, the first run (nothing to compare with), or a comparison. */
export type CardState = "no-cases" | "no-run" | "first-run" | "compared";

export interface TileVm {
  readonly key: string;
  readonly label: string;
  readonly value: string;
  readonly note: string;
  /** down is red, up is ink, equal and first are faint; there is no green. */
  readonly direction: Delta["direction"] | "none";
}

export interface RowVm {
  readonly caseId: string;
  readonly title: string;
  readonly pattern: string;
  readonly toolUse: string;
  readonly outcome: string;
  readonly trajectory: string;
  readonly now: string;
  readonly delta: string;
  readonly direction: Delta["direction"];
  /** "regressed" is a word before the title. */
  readonly regressed: boolean;
  readonly failed: boolean;
  /** Why the case could not run; "" when it did. */
  readonly error: string;
}

export interface RunVm {
  readonly id: string;
  /** "run 12". */
  readonly name: string;
  readonly when: string;
  /** "finished" or "partial · 9 of 24". */
  readonly status: string;
  readonly partial: boolean;
  readonly score: string;
  readonly cost: string;
  readonly duration: string;
  readonly reading: boolean;
  readonly baseline: boolean;
}

export interface CaseVm {
  readonly id: string;
  readonly title: string;
  readonly tools: string;
  readonly source: string;
}

export interface Fingerprint {
  readonly short: string;
  readonly parts: string;
  /** What changed since the baseline, "" when nothing did or there is no baseline. */
  readonly changed: string;
}

export interface ScoreViewModel {
  readonly status: "loading" | "ready" | "failed";
  readonly error: string;
  readonly projectId: string;
  readonly projectName: string;
  readonly view: View;
  readonly state: CardState;
  readonly caseCount: number;
  readonly runCount: number;
  /** "last run 12 · today 11:40 · 7m 12s", "" with no run. */
  readonly lastRun: string;
  /** A run is going: Run bench is busy. Progress and Cancel are the running banner's (the next ticket). */
  readonly running: { readonly done: number; readonly total: number } | null;
  /** Why the last Run bench was refused; "" when it was not. */
  readonly problem: string;
  readonly runs: readonly RunVm[];
  readonly readName: string;
  readonly baselineName: string;
  readonly fingerprint: Fingerprint | null;
  readonly tiles: readonly TileVm[];
  /** "No run yet. Run bench scores all 24 cases…", for the state it belongs to. */
  readonly notice: string;
  /** The setup is the same but the score moved. */
  readonly noise: string;
  readonly filter: Filter;
  readonly filterCounts: Readonly<Record<Filter, number>>;
  readonly rows: readonly RowVm[];
  /** Patterns of phase 2, listed muted. */
  readonly later: readonly string[];
  readonly cases: readonly CaseVm[];
}

export interface ScoreDeps {
  readonly source: ScoreSource;
  readonly feed: ScoreFeed;
  readonly projectId: string;
  readonly projectName: string;
  readonly now: () => Date;
}

const LATER = ["multi-turn · phase 2", "multi-agent · phase 2"];

const loadingVm = (deps: ScoreDeps): ScoreViewModel => ({
  status: "loading",
  error: "",
  projectId: deps.projectId,
  projectName: deps.projectName,
  view: "scorecard",
  state: "no-cases",
  caseCount: 0,
  runCount: 0,
  lastRun: "",
  running: null,
  problem: "",
  runs: [],
  readName: "",
  baselineName: "",
  fingerprint: null,
  tiles: [],
  notice: "",
  noise: "",
  filter: "all",
  filterCounts: { all: 0, regressed: 0, improved: 0, failed: 0, new: 0 },
  rows: [],
  later: LATER,
  cases: [],
});

export class ScorePresenter extends Presenter<ScoreViewModel> {
  readonly #deps: ScoreDeps;
  #cases: BenchCase[] = [];
  #cards: Scorecard[] = [];
  #readId = "";
  #baselineId: string | null = null;
  #view: View = "scorecard";
  #filter: Filter = "all";
  #running: { done: number; total: number } | null = null;
  #problem = "";
  #error = "";
  #status: ScoreViewModel["status"] = "loading";
  #stop: (() => void) | null = null;
  #abort: AbortController | null = null;

  constructor(deps: ScoreDeps) {
    super(loadingVm(deps));
    this.#deps = deps;
  }

  /** Reads the cases and scorecards and follows the project's bench run changes. */
  async start(): Promise<void> {
    this.#stop?.();
    this.#stop = this.#deps.feed.follow(this.#deps.projectId, {
      onRun: (change) => this.#onRun(change),
      onStatus: (status) => {
        if (status === "resynced") void this.#reload();
      },
    });
    await this.#load();
  }

  stop(): void {
    this.#stop?.();
    this.#stop = null;
    this.#abort?.abort();
  }

  /** Reads again after a failure. */
  retry(): Promise<void> {
    this.#status = "loading";
    this.#publish();
    return this.#load();
  }

  setView(view: View): void {
    this.#view = view;
    this.#publish();
  }

  setFilter(filter: Filter): void {
    this.#filter = filter;
    this.#publish();
  }

  /** Reads another run; its baseline goes back to the previous finished one. */
  selectRead(id: string): void {
    if (!this.#cards.some((c) => c.id === id)) return;
    this.#readId = id;
    this.#baselineId = null;
    this.#view = "scorecard";
    this.#publish();
  }

  /** Compares with another run; null goes back to the previous finished one. */
  selectBaseline(id: string | null): void {
    if (id !== null && (id === this.#readId || !this.#cards.some((c) => c.id === id))) return;
    this.#baselineId = id;
    this.#publish();
  }

  /** Run bench: starts a run on the default setup. A refusal is shown and nothing runs. */
  async runBench(): Promise<void> {
    if (this.#running !== null) return;
    this.#problem = "";
    this.#running = { done: 0, total: this.#cases.length };
    this.#publish();
    try {
      const run = await this.#deps.source.startRun(this.#deps.projectId);
      // A change the feed already announced (the run may be over) wins over the answer.
      if (this.#running !== null && run.status === "running") this.#running = { done: run.done, total: run.total };
    } catch (err) {
      this.#running = null;
      this.#problem = err instanceof ScoreError ? err.message : "Run bench failed.";
    }
    this.#publish();
  }

  #onRun(change: RunChange): void {
    if (change.status === "running") {
      this.#running = { done: change.done, total: change.total };
      this.#publish();
      return;
    }
    this.#running = null;
    this.#publish();
    void this.#reload(true);
  }

  async #load(): Promise<void> {
    this.#abort?.abort();
    const abort = new AbortController();
    this.#abort = abort;
    try {
      const [cases, cards] = await Promise.all([this.#deps.source.cases(this.#deps.projectId, abort.signal), this.#deps.source.scorecards(this.#deps.projectId, abort.signal)]);
      this.#cases = cases;
      this.#cards = cards;
      this.#readId = cards[0]?.id ?? "";
      this.#baselineId = null;
      this.#status = "ready";
      this.#error = "";
    } catch (err) {
      if (err instanceof Error && err.name === "AbortError") return;
      this.#status = "failed";
      this.#error = err instanceof ScoreError ? (err.code === "UNAVAILABLE" ? "The harness has no eval bench configured." : err.message) : "The scorecards could not be read.";
    }
    this.#publish();
  }

  /** Reads the scorecards again, keeping the view; `latest` reads the newest one (a run just ended). */
  async #reload(latest = false): Promise<void> {
    try {
      const [cases, cards] = await Promise.all([this.#deps.source.cases(this.#deps.projectId), this.#deps.source.scorecards(this.#deps.projectId)]);
      this.#cases = cases;
      this.#cards = cards;
      if (latest || !cards.some((c) => c.id === this.#readId)) {
        this.#readId = cards[0]?.id ?? "";
        this.#baselineId = null;
      }
      this.#status = "ready";
    } catch {
      // What is on screen stays; the next change or a reload reads again.
    }
    this.#publish();
  }

  #publish(): void {
    this.set(this.#build());
  }

  #build(): ScoreViewModel {
    const deps = this.#deps;
    const now = deps.now();
    const base = loadingVm(deps);
    const cards = this.#cards;
    const asc = [...cards].sort((a, b) => a.startedAt.getTime() - b.startedAt.getTime());
    const name = (c: Scorecard): string => `run ${asc.indexOf(c) + 1}`;
    const read = cards.find((c) => c.id === this.#readId) ?? null;
    const baseline = read === null ? null : this.#baselineId !== null ? (cards.find((c) => c.id === this.#baselineId) ?? null) : defaultBaseline(cards, read);
    const cmp: Comparison | null = read === null ? null : compare(read, baseline);
    const caseTotal = this.#cases.length;
    const state: CardState = caseTotal === 0 && read === null ? "no-cases" : read === null ? "no-run" : baseline === null ? "first-run" : "compared";
    const runs: RunVm[] = cards.map((c) => ({
      id: c.id,
      name: name(c),
      when: when(c.startedAt, now),
      status: c.status === "cancelled" ? `partial · ${c.cases.length} of ${Math.max(caseTotal, c.cases.length)}` : "finished",
      partial: c.status === "cancelled",
      score: c.aggregate.scored === 0 ? "—" : score(c.aggregate.mean),
      cost: cost(c.costUsd),
      duration: duration(c.durationMs),
      reading: c.id === this.#readId,
      baseline: baseline !== null && c.id === baseline.id,
    }));
    const rows = cmp === null ? [] : cmp.rows.map(toRow);
    const counts: Record<Filter, number> = { all: rows.length, regressed: 0, improved: 0, failed: 0, new: 0 };
    if (cmp !== null) for (const r of cmp.rows) for (const s of r.standings) counts[s] += 1;
    return {
      ...base,
      status: this.#status,
      error: this.#error,
      view: this.#view,
      state,
      caseCount: caseTotal,
      runCount: cards.length,
      lastRun: cards[0] ? `last ${name(cards[0])} · ${when(cards[0].startedAt, now)} · ${duration(cards[0].durationMs)}` : "",
      running: this.#running,
      problem: this.#problem,
      runs,
      readName: read ? `${name(read)} · ${when(read.startedAt, now)}` : "",
      baselineName: baseline ? `${name(baseline)} · ${when(baseline.startedAt, now)}` : "",
      fingerprint: read === null || cmp === null ? null : { short: read.setup.fingerprint.slice(0, 6), parts: [read.setup.agentName && `agent ${read.setup.agentName}`, read.setup.model && `model ${read.setup.model}`, read.setup.cliVersion && `claude ${read.setup.cliVersion}`].filter(Boolean).join(" · "), changed: cmp.changedSetup.join(", ") },
      tiles: read === null || cmp === null ? [] : tiles(read, cmp, baseline, baseline ? name(baseline) : ""),
      notice: noticeFor(state, caseTotal),
      noise: cmp?.noiseOnly && baseline && read ? `Same setup as ${name(baseline)} (${read.setup.fingerprint.slice(0, 6)}) but the aggregate moved by ${cmp.aggregate.delta.change.toFixed(2)}. Runs are not deterministic; compare over several runs before acting.` : "",
      filter: this.#filter,
      filterCounts: counts,
      rows: this.#filter === "all" ? rows : rows.filter((_, i) => cmp?.rows[i]?.standings.includes(this.#filter as Standing)),
      cases: this.#cases.map((c) => ({ id: c.id, title: c.name !== "" ? c.name : c.prompt.slice(0, 80), tools: `${c.tools.length} ${c.tools.length === 1 ? "tool" : "tools"}`, source: c.source === "trajectory" ? "recorded" : "written" })),
    };
  }
}

const noticeFor = (state: CardState, cases: number): string => (state === "no-run" ? `No run yet. Run bench scores all ${cases} ${cases === 1 ? "case" : "cases"} against the current setup, with real model calls: it costs money, so it runs only when you ask.` : "");

function toRow(r: CaseRow): RowVm {
  return { caseId: r.caseId, title: r.title, pattern: GROUP_LABEL[r.pattern], toolUse: score(r.toolUse), outcome: score(r.outcome), trajectory: score(r.trajectory), now: score(r.now), delta: deltaShort(r.delta), direction: r.delta.direction, regressed: r.standings.includes("regressed"), failed: r.standings.includes("failed"), error: r.error };
}

function tiles(read: Scorecard, cmp: Comparison, baseline: Scorecard | null, baseName: string): TileVm[] {
  const figure = (key: string, label: string, f: Comparison["aggregate"]): TileVm => {
    const d = f.delta;
    const detail = d.direction === "up" || d.direction === "down" ? `${deltaShort(d)}${key === "aggregate" ? ` vs ${baseName} (${score(d.before)})` : ""}` : d.direction === "first" ? "first run" : `= ${baseName}`;
    return { key, label, value: score(f.value), note: detail, direction: d.direction };
  };
  const spent = baseline ? `${baseName} ${cost(baseline.costUsd)} · ${duration(read.durationMs)}` : duration(read.durationMs);
  return [
    figure("aggregate", "Aggregate score", cmp.aggregate),
    figure("tool_use", "Tool use", cmp.toolUse),
    figure("outcome", "Outcome", cmp.outcome),
    figure("trajectory", "Trajectory", cmp.trajectory),
    { key: "cases", label: "Cases", value: String(read.cases.length), note: baseline ? `${cmp.regressed} regressed · ${cmp.improved} improved · ${cmp.added} new` : "first run", direction: "none" },
    { key: "cost", label: "Estimated cost", value: cost(read.costUsd), note: spent, direction: "none" },
  ];
}
