// DTOs to the Score domain.

import type { BenchCase, BenchRun, RunChange, Trajectory } from "../domain/bench";
import type { ScoreComparison, ScoreDelta } from "../domain/comparison";
import type { Aggregate, CaseScore, Group, Pattern, Scorecard } from "../domain/scorecard";
import type { BenchCaseDto, BenchRunDto, RunChangeDto, TrajectoryDto } from "./dto/bench-dto";
import type { ComparisonDto, DeltaDto, ScorecardDto } from "./dto/scorecard-dto";

export const toCase = (dto: BenchCaseDto): BenchCase => ({
  id: dto.id,
  name: dto.name,
  prompt: dto.prompt,
  tools: dto.tools.map((t) => t.name),
  mocks: dto.tools.map((t) => ({ name: t.name, description: t.description, result: t.result, isError: t.is_error })),
  expect: { tools: dto.expect.tools.map((t) => ({ tool: t.tool, params: t.params })), ordered: dto.expect.ordered, reference: dto.expect.reference },
  source: dto.source,
});

export const toTrajectory = (dto: TrajectoryDto): Trajectory => ({ id: dto.id, taskId: dto.task_id, prompt: dto.prompt, steps: dto.steps, recordedAt: dto.recorded_at });

export const toRun = (dto: BenchRunDto): BenchRun => ({ id: dto.id, status: dto.status, total: dto.total, done: dto.done, scorecardId: dto.scorecard_id, error: dto.error, startedAt: dto.started_at });

export const toRunChange = (dto: RunChangeDto): RunChange => ({ runId: dto.run_id, status: dto.status, done: dto.done, total: dto.total, scorecardId: dto.scorecard_id });

export function toScorecard(dto: ScorecardDto): Scorecard {
  const patterns: Partial<Record<Pattern, Aggregate>> = {};
  for (const [key, a] of Object.entries(dto.patterns)) patterns[key as Pattern] = { scored: a.scored, mean: a.mean };
  const cases: CaseScore[] = dto.cases.map((c) => ({
    caseId: c.case_id,
    caseName: c.case_name,
    patterns: { right_tool: c.scores.right_tool, valid_params: c.scores.valid_params, right_order: c.scores.right_order, outcome: c.scores.outcome, trajectory: c.scores.trajectory },
    error: c.error,
    costUsd: c.cost_usd,
    durationMs: c.duration_ms,
    calls: c.calls.map((x) => ({ tool: x.tool, params: x.params })),
    steps: c.steps.map((x) => ({ step: x.step, ok: x.ok, note: x.note })),
    reasons: { right_tool: c.scores.right_tool.reasons, valid_params: c.scores.valid_params.reasons, right_order: c.scores.right_order.reasons, outcome: c.scores.outcome.reasons, trajectory: c.scores.trajectory.reasons },
    finalText: c.final_text,
  }));
  return {
    id: dto.id,
    status: dto.status,
    setup: { fingerprint: dto.fingerprint, agentName: dto.agent_name, model: dto.model, judgeModel: dto.judge_model, cliVersion: dto.cli_version, agents: dto.agents, skills: dto.skills, instructions: dto.instructions },
    cases,
    patterns,
    aggregate: { scored: dto.aggregate.scored, mean: dto.aggregate.mean },
    costUsd: dto.cost_usd,
    durationMs: dto.duration_ms,
    startedAt: dto.started_at,
    finishedAt: dto.finished_at,
  };
}

const toDelta = (d: DeltaDto): ScoreDelta => ({ before: d.before, after: d.after, change: d.change });

export function toComparison(dto: ComparisonDto): ScoreComparison {
  const groups: Partial<Record<Group, ScoreDelta>> = {};
  for (const [key, d] of Object.entries(dto.groups)) groups[key as Group] = toDelta(d);
  return {
    previousId: dto.previous_id,
    fingerprintChanged: dto.fingerprint_changed,
    changedParts: dto.changed_parts,
    aggregate: toDelta(dto.aggregate),
    groups,
    cases: dto.cases.map((c) => ({ caseId: c.case_id, status: c.status, mean: c.mean === null ? null : toDelta(c.mean) })),
  };
}
