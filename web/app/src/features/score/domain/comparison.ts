// A scorecard read beside a baseline (UX study, section 7): deltas where down is red and nothing is green, and the case rows with how each stands.

import { caseGroup, caseMean, cardGroup, GROUPS, type CaseScore, type Group, type Scorecard } from "./scorecard";

/** A score is shown to two places, so two scores that show the same are equal. */
export const round2 = (n: number): number => Math.round(n * 100) / 100;

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

export function delta(before: number | null, after: number | null): Delta {
  if (after === null || before === null) return { direction: "equal", change: 0, before };
  const change = round2(after) - round2(before);
  if (Math.abs(change) < 0.005) return { direction: "equal", change: 0, before };
  return { direction: change > 0 ? "up" : "down", change: Math.abs(round2(change)), before };
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

/** The run to compare with by default: the newest finished one before `read`. A cancelled run holds only some cases, so it is never one. */
export function defaultBaseline(cards: readonly Scorecard[], read: Scorecard): Scorecard | null {
  const older = cards.filter((c) => c.id !== read.id && c.status === "finished" && c.startedAt.getTime() < read.startedAt.getTime());
  older.sort((a, b) => b.startedAt.getTime() - a.startedAt.getTime());
  return older[0] ?? null;
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

function figure(baseline: Scorecard | null, value: number | null, before: number | null): TileFigure {
  return { value, delta: baseline === null ? FIRST_RUN : delta(before, value) };
}

function changedParts(base: Scorecard, read: Scorecard): string[] {
  if (base.setup.fingerprint === read.setup.fingerprint) return [];
  const parts: string[] = [];
  if (base.setup.agentName !== read.setup.agentName) parts.push("agent");
  if (base.setup.model !== read.setup.model) parts.push("model");
  if (base.setup.judgeModel !== read.setup.judgeModel) parts.push("judge model");
  if (base.setup.cliVersion !== read.setup.cliVersion) parts.push("claude CLI");
  // The fingerprint also covers agents, skills and instructions, which the record does not name.
  return parts.length > 0 ? parts : ["agents, skills or instructions"];
}

export function compare(read: Scorecard, baseline: Scorecard | null): Comparison {
  const before = new Map((baseline?.cases ?? []).map((c) => [c.caseId, c]));
  const rows: CaseRow[] = read.cases.map((c) => {
    const now = caseMean(c);
    const prev = before.get(c.caseId);
    const d: Delta = baseline === null ? FIRST_RUN : prev === undefined ? { direction: "new", change: 0, before: null } : delta(caseMean(prev), now);
    const standings: Standing[] = [];
    if (d.direction === "down") standings.push("regressed");
    if (d.direction === "up") standings.push("improved");
    if (c.error !== "" || (now !== null && now < FAIL_UNDER)) standings.push("failed");
    if (d.direction === "new") standings.push("new");
    return { caseId: c.caseId, title: title(c), pattern: weakest(c), toolUse: caseGroup(c, "tool_use"), outcome: caseGroup(c, "outcome"), trajectory: caseGroup(c, "trajectory"), now, delta: d, standings, error: c.error };
  });
  const count = (s: Standing): number => rows.filter((r) => r.standings.includes(s)).length;
  const aggregate = figure(baseline, read.aggregate.scored === 0 ? null : read.aggregate.mean, baseline && baseline.aggregate.scored > 0 ? baseline.aggregate.mean : null);
  const group = (g: Group): TileFigure => figure(baseline, cardGroup(read, g), baseline ? cardGroup(baseline, g) : null);
  const changedSetup = baseline ? changedParts(baseline, read) : [];
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
    noiseOnly: baseline !== null && changedSetup.length === 0 && aggregate.delta.direction !== "equal",
  };
}
