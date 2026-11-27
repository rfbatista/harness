// The Score page: the scorecard of a run beside a baseline, the cases and the runs, and Run bench.

import { Presenter } from "../../../../shared/kernel/presenter";
import type { CallsText, ScoreFeed, ScoreSource } from "../../application/ports";
import { ScoreError, type BenchCase, type CaseDefinition, type FieldError, type RunChange, type Trajectory } from "../../domain/bench";
import { formatMocks, parseMockTools } from "../../domain/case-text";
import { comparisonOf, round2, type CaseRow, type Comparison, type Delta, type ScoreComparison, type Standing } from "../../domain/comparison";
import { cost, deltaShort, duration, score, when } from "../../domain/format";
import { caseMean, GROUP_LABEL, type CaseScore, type Scorecard } from "../../domain/scorecard";

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
  /** Why a run holds only some cases: "cancelled" (stopped) or "cases" (asked for some); "finished" when it holds them all. */
  readonly kind: "finished" | "cancelled" | "cases";
  readonly score: string;
  readonly cost: string;
  readonly duration: string;
  readonly reading: boolean;
  readonly baseline: boolean;
}

/** One step of a case's trace: the call in mono and the judge's word for it. A step the run never made has no call. */
export interface StepVm {
  readonly n: number;
  readonly call: string;
  readonly note: string;
  /** Red only where the judge failed the step. */
  readonly failed: boolean;
}

export interface CaseDetailVm {
  readonly prompt: string;
  readonly steps: readonly StepVm[];
  /** The judge's paragraph, or why there is none. */
  readonly judge: string;
  readonly finalText: string;
  /** The case's score in the run read, and in the baseline: "—" when not scored. */
  readonly now: string;
  readonly baseline: string;
  readonly expectation: { readonly calls: string; readonly ordered: boolean; readonly reference: string };
  readonly mocks: string;
}

export interface CaseVm {
  readonly id: string;
  readonly title: string;
  /** What the case is scored on: "tool use", "outcome". */
  readonly pattern: string;
  readonly tools: string;
  readonly source: string;
  /** The score in the run read, "—" when none scored it. */
  readonly score: string;
  /** The last five runs' scores, oldest first (0 to 1). */
  readonly trend: readonly number[];
  /** Open in place to its trace. */
  readonly trace: CaseDetailVm | null;
  readonly confirmingDelete: boolean;
}

export type FormErrorKey = "name" | "prompt" | "mocks" | "calls" | "reference" | "general";

/** The Add case and Edit expectation form: text the person types, parsed on Save. */
export interface FormVm {
  readonly mode: "add" | "edit";
  readonly caseId: string;
  readonly title: string;
  readonly name: string;
  readonly prompt: string;
  readonly mocks: string;
  readonly calls: string;
  readonly ordered: boolean;
  readonly reference: string;
  readonly errors: Readonly<Partial<Record<FormErrorKey, string>>>;
  readonly saving: boolean;
}

/** The recorded trajectories a case can be made from. */
export interface TrajectoriesVm {
  readonly loading: boolean;
  readonly items: readonly { readonly id: string; readonly prompt: string; readonly steps: string; readonly when: string }[];
  /** What to do when there is none. */
  readonly empty: string;
  readonly error: string;
}

export interface Fingerprint {
  readonly short: string;
  readonly parts: string;
  /** What changed since the baseline, "" when nothing did or there is no baseline. */
  readonly changed: string;
}

export interface RunningVm {
  readonly runId: string;
  readonly done: number;
  readonly total: number;
  /** Cases done as a share of the cases it will score, 0 to 100. */
  readonly percent: number;
  /** "2m 40s" since the run started. */
  readonly elapsed: string;
  /** Cancel was asked and the harness has not ended the run yet. */
  readonly cancelling: boolean;
}

export interface FailedRunVm {
  /** "Run 13 failed". */
  readonly title: string;
  /** Why the harness says it failed; "" when it did not say. */
  readonly message: string;
  readonly done: number;
  readonly total: number;
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
  /** A run is going: Run bench is replaced by Cancel, and the banner counts it. `runId` is "" until the harness has named the run. */
  readonly running: RunningVm | null;
  /** The last run failed: the danger banner, with Retry. */
  readonly failedRun: FailedRunVm | null;
  /** Why the last Run bench (or Cancel) was refused; "" when it was not. */
  readonly problem: string;
  readonly problemTitle: string;
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
  readonly form: FormVm | null;
  readonly trajectories: TrajectoriesVm | null;
}

export interface ScoreDeps {
  readonly source: ScoreSource;
  readonly feed: ScoreFeed;
  readonly calls: CallsText;
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
  failedRun: null,
  problem: "",
  problemTitle: "",
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
  form: null,
  trajectories: null,
});

export class ScorePresenter extends Presenter<ScoreViewModel> {
  readonly #deps: ScoreDeps;
  #cases: BenchCase[] = [];
  #cards: Scorecard[] = [];
  #readId = "";
  #baselineId: string | null = null;
  /** What the harness answered for the run read (null: nothing earlier to compare with); only while it is for `readId`. */
  #comparison: { readId: string; answer: ScoreComparison | null } | null = null;
  #comparing = 0;
  #view: View = "scorecard";
  #filter: Filter = "all";
  #running: { runId: string; done: number; total: number; startedAt: Date; cancelling: boolean } | null = null;
  #failure: { runId: string; scorecardId: string; message: string; done: number; total: number } | null = null;
  #problem = "";
  #problemTitle = "";
  #error = "";
  #open = "";
  #confirming = "";
  #form: Omit<FormVm, "title"> | null = null;
  #trajectories: { items: Trajectory[]; loading: boolean; error: string } | null = null;
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
  async selectRead(id: string): Promise<void> {
    if (!this.#cards.some((c) => c.id === id)) return;
    this.#readId = id;
    this.#baselineId = null;
    this.#view = "scorecard";
    this.#publish();
    await this.#compare();
    this.#publish();
  }

  /** Compares with another run; null goes back to the previous finished one. */
  async selectBaseline(id: string | null): Promise<void> {
    if (id !== null && (id === this.#readId || !this.#cards.some((c) => c.id === id))) return;
    this.#baselineId = id;
    await this.#compare();
    this.#publish();
  }

  /** Asks the harness for the run read set against its baseline. A newer ask wins; what was on screen stays until the answer lands. */
  async #compare(): Promise<void> {
    const readId = this.#readId;
    const ask = ++this.#comparing;
    if (readId === "") {
      this.#comparison = null;
      return;
    }
    let answer: ScoreComparison | null = null;
    try {
      answer = await this.#deps.source.compare(this.#deps.projectId, readId, this.#baselineId);
    } catch {
      this.#problem = "The comparison could not be read.";
    }
    if (ask === this.#comparing) this.#comparison = { readId, answer };
  }

  /** Run bench: starts a run on the default setup. A refusal is shown and nothing runs. */
  runBench(): Promise<void> {
    return this.#start(undefined);
  }

  /** Replay this case: a run of that one case, shown as running like Run bench. Its scorecard is partial, never a baseline. */
  replayCase(id: string): Promise<void> {
    return this.#start([id]);
  }

  async #start(caseIds: readonly string[] | undefined): Promise<void> {
    if (this.#running !== null) return;
    this.#problem = "";
    this.#failure = null;
    this.#running = { runId: "", done: 0, total: caseIds?.length ?? this.#cases.length, startedAt: this.#deps.now(), cancelling: false };
    this.#publish();
    try {
      const run = await this.#deps.source.startRun(this.#deps.projectId, caseIds);
      // A change the feed already announced (the run may be over) wins over the answer.
      if (this.#running !== null && run.status === "running") this.#running = { ...this.#running, runId: run.id, done: Math.max(this.#running.done, run.done), total: run.total, startedAt: run.startedAt ?? this.#running.startedAt };
    } catch (err) {
      this.#running = null;
      this.#problemTitle = "Run bench did not start.";
      this.#problem = err instanceof ScoreError ? err.message : "Run bench failed.";
    }
    this.#publish();
  }

  /** Cancel: asks the harness to stop the run; the run ends, partial, when the feed says so. A refusal is shown and the run stays. */
  async cancelRun(): Promise<void> {
    const run = this.#running;
    if (run === null || run.runId === "" || run.cancelling) return;
    this.#problem = "";
    this.#running = { ...run, cancelling: true };
    this.#publish();
    try {
      await this.#deps.source.cancelRun(this.#deps.projectId, run.runId);
    } catch (err) {
      if (this.#running !== null) this.#running = { ...this.#running, cancelling: false };
      this.#problemTitle = "Cancel did not stop the run.";
      this.#problem = err instanceof ScoreError ? err.message : "Cancel failed.";
    }
    this.#publish();
  }

  /** Retry after a failed run: a new run on the default setup. */
  retryRun(): Promise<void> {
    return this.#start(undefined);
  }

  dismissFailure(): void {
    this.#failure = null;
    this.#publish();
  }

  /** The clock moved: the elapsed time of a running run reads again. */
  tick(): void {
    if (this.#running !== null) this.#publish();
  }

  /** Opens a case in place to its trace; opening it again closes it. */
  openCase(id: string): void {
    this.#open = this.#open === id ? "" : id;
    this.#publish();
  }

  /** Edit expectation: the form starts from what the case expects. */
  startEdit(id: string): void {
    const found = this.#cases.find((c) => c.id === id);
    if (!found) return;
    this.#form = { mode: "edit", caseId: id, name: found.name, prompt: found.prompt, mocks: formatMocks(found.mocks), calls: this.#deps.calls.format(found.expect.tools), ordered: found.expect.ordered, reference: found.expect.reference, errors: {}, saving: false };
    this.#publish();
  }

  /** Add case: an empty definition. */
  startAdd(): void {
    this.#form = { mode: "add", caseId: "", name: "", prompt: "", mocks: "", calls: "", ordered: false, reference: "", errors: {}, saving: false };
    this.#publish();
  }

  /** What the person types in the form; its errors stay until Save checks again. */
  setForm(change: Partial<Pick<FormVm, "name" | "prompt" | "mocks" | "calls" | "ordered" | "reference">>): void {
    if (this.#form === null) return;
    this.#form = { ...this.#form, ...change };
    this.#publish();
  }

  cancelForm(): void {
    this.#form = null;
    this.#publish();
  }

  /** Save: the text is parsed first (a bad line is shown on its field and nothing is sent), then the harness validates it; its refusal lands on the field it names and the typed text stays. */
  async saveForm(): Promise<void> {
    const form = this.#form;
    if (form === null || form.saving) return;
    const errors: Partial<Record<FormErrorKey, string>> = {};
    const calls = this.#deps.calls.parse(form.calls);
    const mocks = parseMockTools(form.mocks);
    if (calls.errors.length > 0) errors.calls = calls.errors.map((e) => e.message).join(" ");
    if (form.mode === "add" && mocks.errors.length > 0) errors.mocks = mocks.errors.map((e) => e.message).join(" ");
    if (Object.keys(errors).length > 0) {
      this.#form = { ...form, errors };
      this.#publish();
      return;
    }
    this.#form = { ...form, errors: {}, saving: true };
    this.#publish();
    const expect = { tools: calls.calls, ordered: form.ordered, reference: form.reference };
    try {
      if (form.mode === "edit") await this.#deps.source.updateExpectation(this.#deps.projectId, form.caseId, expect);
      else await this.#deps.source.createCase(this.#deps.projectId, { name: form.name, prompt: form.prompt, mocks: mocks.mocks, expect } satisfies CaseDefinition);
    } catch (err) {
      this.#form = { ...form, errors: refusal(err), saving: false };
      this.#publish();
      return;
    }
    this.#form = null;
    await this.#reload();
  }

  /** Delete asks once: the first call marks the case, confirmDelete removes it. */
  requestDelete(id: string): void {
    this.#confirming = id;
    this.#publish();
  }

  cancelDelete(): void {
    this.#confirming = "";
    this.#publish();
  }

  async confirmDelete(id: string): Promise<void> {
    if (this.#confirming !== id) return;
    this.#confirming = "";
    try {
      await this.#deps.source.deleteCase(this.#deps.projectId, id);
    } catch (err) {
      this.#problem = err instanceof ScoreError ? err.message : "The case was not deleted.";
    }
    if (this.#open === id) this.#open = "";
    await this.#reload();
  }

  /** From a recorded trajectory: lists the sessions recorded for tasks that opted in. */
  async openTrajectories(): Promise<void> {
    this.#trajectories = { items: [], loading: true, error: "" };
    this.#publish();
    try {
      this.#trajectories = { items: await this.#deps.source.trajectories(this.#deps.projectId), loading: false, error: "" };
    } catch (err) {
      this.#trajectories = { items: [], loading: false, error: err instanceof ScoreError ? err.message : "The recordings could not be read." };
    }
    this.#publish();
  }

  closeTrajectories(): void {
    this.#trajectories = null;
    this.#publish();
  }

  async chooseTrajectory(id: string): Promise<void> {
    try {
      await this.#deps.source.caseFromTrajectory(this.#deps.projectId, id);
    } catch (err) {
      this.#trajectories = { items: this.#trajectories?.items ?? [], loading: false, error: err instanceof ScoreError ? err.message : "The case was not made." };
      this.#publish();
      return;
    }
    this.#trajectories = null;
    await this.#reload();
  }

  #onRun(change: RunChange): void {
    if (change.status === "running") {
      const mine = this.#running;
      // One run at a time per project: a change of another run than ours, while ours is going, is not shown.
      if (mine !== null && mine.runId !== "" && mine.runId !== change.runId) return;
      this.#failure = null;
      this.#running = { runId: change.runId, done: change.done, total: change.total, startedAt: mine?.startedAt ?? this.#deps.now(), cancelling: mine?.cancelling ?? false };
      if (mine === null || mine.runId === "") void this.#learnStart(change.runId);
      this.#publish();
      return;
    }
    this.#running = null;
    this.#publish();
    void this.#ended(change);
  }

  /** A run seen only on the feed: asks the harness when it started, so the elapsed time is the run's own. */
  async #learnStart(runId: string): Promise<void> {
    try {
      const run = await this.#deps.source.run(this.#deps.projectId, runId);
      if (this.#running?.runId === runId && run.startedAt !== null) {
        this.#running = { ...this.#running, startedAt: run.startedAt };
        this.#publish();
      }
    } catch {
      // The elapsed time counts from when this page first saw the run.
    }
  }

  /** The run is over: what it scored is read, and a failed run says why. */
  async #ended(change: RunChange): Promise<void> {
    const reading = this.#reload(true);
    if (change.status === "failed") {
      let message = "";
      try {
        message = (await this.#deps.source.run(this.#deps.projectId, change.runId)).error;
      } catch {
        // The banner still says the run failed.
      }
      this.#failure = { runId: change.runId, scorecardId: change.scorecardId, message, done: change.done, total: change.total };
    }
    await reading;
    this.#publish();
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
      await this.#compare();
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
      await this.#compare();
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
    const answer = read !== null && this.#comparison?.readId === read.id ? this.#comparison.answer : null;
    const baseline = answer === null ? null : (cards.find((c) => c.id === answer.previousId) ?? null);
    const cmp: Comparison | null = read === null ? null : comparisonOf(read, baseline, answer);
    const caseTotal = this.#cases.length;
    const state: CardState = caseTotal === 0 && read === null ? "no-cases" : read === null ? "no-run" : baseline === null ? "first-run" : "compared";
    const runs: RunVm[] = cards.map((c) => ({
      id: c.id,
      name: name(c),
      when: when(c.startedAt, now),
      status: c.status !== "finished" ? `partial · ${c.cases.length} of ${Math.max(caseTotal, c.cases.length)}` : "finished",
      partial: c.status !== "finished",
      kind: c.status === "cancelled" ? "cancelled" : c.status === "partial" ? "cases" : "finished",
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
      running: this.#running === null ? null : { runId: this.#running.runId, done: this.#running.done, total: this.#running.total, percent: this.#running.total === 0 ? 0 : Math.min(100, Math.floor((this.#running.done * 100) / this.#running.total)), elapsed: duration(Math.max(0, now.getTime() - this.#running.startedAt.getTime())), cancelling: this.#running.cancelling },
      failedRun: this.#failure === null ? null : { title: `${failedName(this.#failure.scorecardId, cards)} failed`, message: this.#failure.message, done: this.#failure.done, total: this.#failure.total },
      problem: this.#problem,
      problemTitle: this.#problemTitle,
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
      cases: this.#cases.map((c) => this.#caseVm(c, asc, read, baseline)),
      form: this.#form === null ? null : { ...this.#form, title: this.#form.mode === "add" ? "Add case" : `Edit expectation · ${this.#cases.find((c) => c.id === this.#form?.caseId)?.name || "case"}` },
      trajectories: this.#trajectoriesVm(now),
    };
  }
  #caseVm(c: BenchCase, asc: readonly Scorecard[], read: Scorecard | null, baseline: Scorecard | null): CaseVm {
    const scored = (card: Scorecard | null): CaseScore | undefined => card?.cases.find((x) => x.caseId === c.id);
    const mean = (x: CaseScore | undefined): number | null => (x === undefined ? null : caseMean(x));
    const trend = asc.flatMap((card) => {
      const m = mean(scored(card));
      return m === null ? [] : [round2(m)];
    }).slice(-5);
    const here = scored(read);
    const pattern = [c.expect.tools.length > 0 && "tool use", c.expect.reference !== "" && "outcome"].filter(Boolean).join(" · ");
    return {
      id: c.id,
      title: c.name !== "" ? c.name : c.prompt.slice(0, 80),
      pattern,
      tools: `${c.tools.length} ${c.tools.length === 1 ? "tool" : "tools"}`,
      source: c.source === "trajectory" ? "recorded" : "written",
      score: score(mean(here)),
      trend,
      trace: this.#open === c.id ? detailOf(c, here, scored(baseline), this.#deps.calls) : null,
      confirmingDelete: this.#confirming === c.id,
    };
  }

  #trajectoriesVm(now: Date): TrajectoriesVm | null {
    const t = this.#trajectories;
    if (t === null) return null;
    return {
      loading: t.loading,
      items: t.items.map((x) => ({ id: x.id, prompt: x.prompt, steps: `${x.steps} ${x.steps === 1 ? "step" : "steps"}`, when: when(x.recordedAt, now) })),
      empty: "No session has been recorded yet. Trajectory recording is set per task, in its Edit form (the project default is in Agent system, under Score); a session of a task that opted in shows here once it ends.",
      error: t.error,
    };
  }
}

/** The trace of a case in a run: each call the run made beside the judge's note for that step; notes for steps it never made follow. */
function detailOf(c: BenchCase, here: CaseScore | undefined, before: CaseScore | undefined, text: CallsText): CaseDetailVm {
  const expectation = { calls: text.format(c.expect.tools), ordered: c.expect.ordered, reference: c.expect.reference };
  const base = { prompt: c.prompt, expectation, mocks: formatMocks(c.mocks) };
  if (here === undefined) return { ...base, steps: [], judge: "No run has scored this case yet.", finalText: "", now: "—", baseline: score(before === undefined ? null : caseMean(before)) };
  const notes = new Map(here.steps.map((n) => [n.step, n]));
  const steps: StepVm[] = here.calls.map((call, i) => {
    const note = notes.get(i + 1);
    notes.delete(i + 1);
    return { n: i + 1, call: text.formatCall(call.tool, call.params), note: note?.note ?? "", failed: note !== undefined && !note.ok };
  });
  for (const n of [...notes.values()].sort((a, b) => a.step - b.step)) steps.push({ n: n.step, call: "", note: n.note, failed: !n.ok });
  const judge = [...(here.reasons.trajectory ?? []), ...(here.reasons.outcome ?? [])].join(" ");
  return { ...base, steps, judge: judge !== "" ? judge : here.error !== "" ? here.error : "The judge left no paragraph for this case.", finalText: here.finalText, now: score(caseMean(here)), baseline: score(before === undefined ? null : caseMean(before)) };
}

/** A refusal's fields onto the form's: the harness names `prompt`, `expect`, `expect.tools[0].tool`, `tools[1].name`. */
function refusal(err: unknown): Partial<Record<FormErrorKey, string>> {
  if (!(err instanceof ScoreError)) return { general: "The case was not saved." };
  if (err.fields.length === 0) return { general: err.message };
  const errors: Partial<Record<FormErrorKey, string>> = {};
  for (const f of err.fields as readonly FieldError[]) {
    const key: FormErrorKey = f.field === "prompt" ? "prompt" : f.field === "name" ? "name" : f.field.startsWith("tools") ? "mocks" : f.field.startsWith("expect") ? "calls" : "general";
    errors[key] = `${errors[key] ? `${errors[key]} ` : ""}${f.code}: ${f.message}`;
  }
  return errors;
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

/** "Run 13": the failed run's scorecard when it kept one, else the run it would have been. */
function failedName(scorecardId: string, cards: readonly Scorecard[]): string {
  const asc = [...cards].sort((a, b) => a.startedAt.getTime() - b.startedAt.getTime());
  const at = asc.findIndex((c) => c.id === scorecardId);
  return `Run ${at >= 0 ? at + 1 : cards.length + 1}`;
}
