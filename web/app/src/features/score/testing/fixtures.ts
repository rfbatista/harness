// Scorecards as the bench API writes them (wire JSON), built from the scores a test cares about.

export interface CaseSpec {
  id: string;
  name?: string;
  /** Tool use score (the three tool patterns all get it); omit for none scored. */
  tool?: number;
  outcome?: number;
  trajectory?: number;
  error?: string;
}

export interface CardSpec {
  id: string;
  /** RFC 3339. */
  at: string;
  status?: "finished" | "cancelled";
  fingerprint?: string;
  model?: string;
  cost?: number;
  durationMs?: number;
  cases: CaseSpec[];
}

const pattern = (score: number | undefined) => (score === undefined ? { scored: false, score: 0 } : { scored: true, score });

/** The wire scorecard: per-pattern aggregates and the overall mean are computed the way the server does. */
export function wireScorecard(spec: CardSpec): Record<string, unknown> {
  const cases = spec.cases.map((c) => ({
    case_id: c.id,
    case_name: c.name ?? c.id,
    tool_use: { right_tool: pattern(c.tool), valid_params: pattern(c.tool), right_order: pattern(c.tool) },
    outcome: pattern(c.outcome),
    trajectory: pattern(c.trajectory),
    calls: [],
    cost_usd: 0.05,
    duration_ms: 1000,
    ...(c.error ? { error: c.error } : {}),
  }));
  const names = ["right_tool", "valid_params", "right_order", "outcome", "trajectory"] as const;
  const patterns: Record<string, { scored: number; mean: number }> = {};
  for (const name of names) {
    const scores = cases.flatMap((c) => {
      const p = name === "outcome" || name === "trajectory" ? c[name] : c.tool_use[name];
      return p.scored ? [p.score] : [];
    });
    if (scores.length > 0) patterns[name] = { scored: scores.length, mean: scores.reduce((a, b) => a + b, 0) / scores.length };
  }
  const means = spec.cases.map((c) => {
    if (c.error) return 0;
    const scores = [c.tool, c.outcome, c.trajectory].filter((s): s is number => s !== undefined);
    return scores.length === 0 ? null : scores.reduce((a, b) => a + b, 0) / scores.length;
  }).filter((m): m is number => m !== null);
  return {
    id: spec.id,
    project_id: "p1",
    status: spec.status ?? "finished",
    setup: { fingerprint: spec.fingerprint ?? "fp1", model: spec.model ?? "opus-5-5", cli_version: "2.1.14" },
    cases,
    patterns,
    aggregate: { scored: means.length, mean: means.length === 0 ? 0 : means.reduce((a, b) => a + b, 0) / means.length },
    cost_usd: spec.cost ?? 1.14,
    duration_ms: spec.durationMs ?? 432_000,
    started_at: spec.at,
    finished_at: spec.at,
  };
}

/** A card with scores the read-side tests use: aggregate = mean over cases. */
export const card = (spec: CardSpec): CardSpec => spec;
