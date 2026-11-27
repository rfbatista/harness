// A bench run's scorecard (ADR 0015, Glossary "Scorecard"): scores per case and per pattern, with cost and duration.

/** The patterns a case is scored on, in the order a scorecard shows them. */
export const PATTERNS = ["right_tool", "valid_params", "right_order", "outcome", "trajectory"] as const;
export type Pattern = (typeof PATTERNS)[number];

/** The three columns of a case row: tool use is the mean of its three patterns. */
export type Group = "tool_use" | "outcome" | "trajectory";
export const GROUPS: readonly Group[] = ["tool_use", "outcome", "trajectory"];
export const GROUP_LABEL: Record<Group, string> = { tool_use: "tool use", outcome: "outcome", trajectory: "trajectory" };
const GROUP_PATTERNS: Record<Group, readonly Pattern[]> = { tool_use: ["right_tool", "valid_params", "right_order"], outcome: ["outcome"], trajectory: ["trajectory"] };

/** A pattern the case expects nothing of, or one that could not be judged, is not scored. */
export interface PatternScore {
  readonly scored: boolean;
  readonly score: number;
}

export interface CaseScore {
  readonly caseId: string;
  readonly caseName: string;
  readonly patterns: Readonly<Record<Pattern, PatternScore>>;
  /** Why the case could not run, "" when it did. */
  readonly error: string;
  readonly costUsd: number;
  readonly durationMs: number;
}

export interface Aggregate {
  readonly scored: number;
  readonly mean: number;
}

/** The setup a scorecard ran on: the fingerprint, and the parts that name it. */
export interface Setup {
  readonly fingerprint: string;
  readonly agentName: string;
  readonly model: string;
  readonly judgeModel: string;
  readonly cliVersion: string;
}

export type ScorecardStatus = "finished" | "cancelled";

export interface Scorecard {
  readonly id: string;
  readonly status: ScorecardStatus;
  readonly setup: Setup;
  readonly cases: readonly CaseScore[];
  readonly patterns: Readonly<Partial<Record<Pattern, Aggregate>>>;
  readonly aggregate: Aggregate;
  readonly costUsd: number;
  readonly durationMs: number;
  readonly startedAt: Date;
  readonly finishedAt: Date;
}

/** A case that could not run is a failure and scores zero; one with nothing scored has no mean. */
export function caseMean(c: CaseScore): number | null {
  if (c.error !== "") return 0;
  const scored = PATTERNS.map((p) => c.patterns[p]).filter((p) => p.scored);
  return scored.length === 0 ? null : scored.reduce((sum, p) => sum + p.score, 0) / scored.length;
}

/** A case's score on one column, null when none of its patterns is scored. */
export function caseGroup(c: CaseScore, group: Group): number | null {
  const scored = GROUP_PATTERNS[group].map((p) => c.patterns[p]).filter((p) => p.scored);
  return scored.length === 0 ? null : scored.reduce((sum, p) => sum + p.score, 0) / scored.length;
}

/** A scorecard's mean on one column, from its per-pattern aggregates (weighted by the cases scored); null when nothing was scored. */
export function cardGroup(card: Scorecard, group: Group): number | null {
  let weight = 0;
  let sum = 0;
  for (const p of GROUP_PATTERNS[group]) {
    const a = card.patterns[p];
    if (!a || a.scored === 0) continue;
    weight += a.scored;
    sum += a.mean * a.scored;
  }
  return weight === 0 ? null : sum / weight;
}
