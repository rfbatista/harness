// DTOs to the Score domain.

import type { BenchCase, BenchRun, RunChange } from "../domain/bench";
import type { Aggregate, CaseScore, Pattern, Scorecard } from "../domain/scorecard";
import type { BenchCaseDto, BenchRunDto, RunChangeDto } from "./dto/bench-dto";
import type { ScorecardDto } from "./dto/scorecard-dto";

export const toCase = (dto: BenchCaseDto): BenchCase => ({ id: dto.id, name: dto.name, prompt: dto.prompt, tools: dto.tools, source: dto.source });

export const toRun = (dto: BenchRunDto): BenchRun => ({ id: dto.id, status: dto.status, total: dto.total, done: dto.done, scorecardId: dto.scorecard_id, error: dto.error });

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
  }));
  return {
    id: dto.id,
    status: dto.status,
    setup: { fingerprint: dto.fingerprint, agentName: dto.agent_name, model: dto.model, judgeModel: dto.judge_model, cliVersion: dto.cli_version },
    cases,
    patterns,
    aggregate: { scored: dto.aggregate.scored, mean: dto.aggregate.mean },
    costUsd: dto.cost_usd,
    durationMs: dto.duration_ms,
    startedAt: dto.started_at,
    finishedAt: dto.finished_at,
  };
}
