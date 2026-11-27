// A scorecard read beside a baseline (UX study, section 7): deltas where down is red and nothing is green, and the case rows with how each stands.
// The harness does the comparison (ADR 0015: the compare endpoint, with an optional baseline); this module reads its answer for display.

import { caseGroup, caseMean, cardGroup, GROUPS, type CaseScore, type Group, type Scorecard } from "./scorecard";

/** A score is shown to two places, so two scores that show the same are equal. */
export const round2 = (n: number): number => Math.round(n * 100) / 100;

/** A score before and after, and the change, as the harness computed them. */
export interface ScoreDelta {
  readonly before: number;
  readonly after: number;
  readonly change: number;
}

/** What the harness answers when a scorecard is set against a baseline. */
export interface ScoreComparison {
  readonly previousId: string;
  readonly fingerprintChanged: boolean;
  /** The setup parts that changed ("model", "instruction AGENTS.md", "skill skills/x/SKILL.md"); null when either run did not record its parts. */
  readonly changedParts: readonly string[] | null;
  readonly aggregate: ScoreDelta;
  readonly groups: Readonly<Partial<Record<Group, ScoreDelta>>>;
  /** Per case of the run read: "added" is new, "unchanged" has no mean delta. */
  readonly cases: readonly { readonly caseId: string; readonly status: string; readonly mean: ScoreDelta | null }[];
}

/** A case under this score has failed. */
export const FAIL_UNDER = 0.6;

export type DeltaDirection = "up" | "down" | "equal" | "first" | "new";

/** What changed since the baseline: a sign, a number and the baseline's value. */
export interface Delta {
  readonly direction: DeltaDirection;
  /** Absolute change at two places, 0 for equal, first and new. */
  readonly change: number;
  readonly before: number | null;
}

/** The harness's delta as it reads on the page: a change that shows as 0.00 reads equal. */
export function deltaOf(d: ScoreDelta | null | undefined): Delta {
  if (d === null || d === undefined) return { direction: "equal", change: 0, before: null };
  if (Math.abs(d.change) < 0.005) return { direction: "equal", change: 0, before: d.before };
  return { direction: d.change > 0 ? "up" : "down", change: Math.abs(d.change), before: d.before };
}

export const FIRST_RUN: Delta = { direction: "first", change: 0, before: null };

/** How a case stands against the baseline; a case may be several (a failed case that also regressed). */
export type Standing = "regressed" | "improved" | "failed" | "new";

export interface CaseRow {
  readonly caseId: string;
  readonly title: string;
  /** The weakest column, the one to open first. */
  readonly pattern: Group;
  readonly toolUse: number | null;
  readonly outcome: number | null;
  readonly trajectory: number | null;
  /** The case's score in this run, null when nothing was scored. */
  readonly now: number | null;
  readonly delta: Delta;
  readonly standings: readonly Standing[];
  /** Why the case could not run; "" when it did. */
  readonly error: string;
}

export interface TileFigure {
  readonly value: number | null;
  readonly delta: Delta;
  /** The harness gave a delta for it (a group nothing scored on either side has none). */
  readonly answered: boolean;
}

export interface Comparison {
  readonly baseline: Scorecard | null;
  readonly aggregate: TileFigure;
  readonly toolUse: TileFigure;
  readonly outcome: TileFigure;
  readonly trajectory: TileFigure;
  readonly rows: readonly CaseRow[];
  readonly regressed: number;
  readonly improved: number;
  readonly failed: number;
  readonly added: number;
  /** Parts of the setup that differ from the baseline's ("model", "agent", "claude CLI", "judge model"); empty when the fingerprints match. */
  readonly changedSetup: readonly string[];
  /** The setup is the same but the aggregate moved: runs are not deterministic. */
  readonly noiseOnly: boolean;
}

const title = (c: CaseScore): string => (c.caseName !== "" ? c.caseName : c.caseId);

function weakest(c: CaseScore): Group {
  let pick: Group = "tool_use";
  let low = Infinity;
  for (const g of GROUPS) {
    const v = caseGroup(c, g);
    if (v !== null && v < low) {
      low = v;
      pick = g;
    }
  }
  return pick;
}

/** A recorded part in the words the setup line uses: a file by its name, a skill by its folder. */
function partWords(part: string): string {
  if (part.startsWith("instruction ")) return part.slice("instruction ".length);
  const skill = /^skill (?:.*\/)?([^/]+)\/SKILL\.md$/.exec(part);
  if (skill !== null) return `skill ${skill[1] ?? ""}`;
  return part === "claude cli" ? "claude CLI" : part;
}

function changedParts(base: Scorecard, read: Scorecard, named: readonly string[] | null): string[] {
  if (base.setup.fingerprint === read.setup.fingerprint) return [];
  if (named !== null) return named.map(partWords);
  const parts: string[] = [];
  if (base.setup.agentName !== read.setup.agentName) parts.push("agent");
  if (base.setup.model !== read.setup.model) parts.push("model");
  if (base.setup.judgeModel !== read.setup.judgeModel) parts.push("judge model");
  if (base.setup.cliVersion !== read.setup.cliVersion) parts.push("claude CLI");
  // The fingerprint also covers agents, skills and instructions, which the record does not name.
  return parts.length > 0 ? parts : ["agents, skills or instructions"];
}

/** The page's view of a scorecard against its baseline: what the harness answered (null: no baseline, a first run), laid out for display. */
export function comparisonOf(read: Scorecard, baseline: Scorecard | null, answer: ScoreComparison | null): Comparison {
  const against = baseline !== null && answer !== null ? answer : null;
  const byCase = new Map((against?.cases ?? []).map((c) => [c.caseId, c]));
  const rows: CaseRow[] = read.cases.map((c) => {
    const now = caseMean(c);
    const known = byCase.get(c.caseId);
    const d: Delta = against === null ? FIRST_RUN : known === undefined || known.status === "added" ? { direction: "new", change: 0, before: null } : deltaOf(known.mean);
    const standings: Standing[] = [];
    if (d.direction === "down") standings.push("regressed");
    if (d.direction === "up") standings.push("improved");
    if (c.error !== "" || (now !== null && now < FAIL_UNDER)) standings.push("failed");
    if (d.direction === "new") standings.push("new");
    return { caseId: c.caseId, title: title(c), pattern: weakest(c), toolUse: caseGroup(c, "tool_use"), outcome: caseGroup(c, "outcome"), trajectory: caseGroup(c, "trajectory"), now, delta: d, standings, error: c.error };
  });
  const count = (s: Standing): number => rows.filter((r) => r.standings.includes(s)).length;
  const figure = (value: number | null, d: ScoreDelta | undefined): TileFigure => ({ value, delta: against === null ? FIRST_RUN : deltaOf(d), answered: against !== null && d !== undefined });
  const aggregate = figure(read.aggregate.scored === 0 ? null : read.aggregate.mean, against?.aggregate);
  const group = (g: Group): TileFigure => figure(cardGroup(read, g), against?.groups[g]);
  const changedSetup = baseline !== null && against !== null && against.fingerprintChanged ? changedParts(baseline, read, against.changedParts) : [];
  return {
    baseline,
    aggregate,
    toolUse: group("tool_use"),
    outcome: group("outcome"),
    trajectory: group("trajectory"),
    rows,
    regressed: count("regressed"),
    improved: count("improved"),
    failed: count("failed"),
    added: count("new"),
    changedSetup,
    noiseOnly: against !== null && !against.fingerprintChanged && aggregate.delta.direction !== "equal",
  };
}
