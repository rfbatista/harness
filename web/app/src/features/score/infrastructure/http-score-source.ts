// ScoreSource over the bench API: cases, scorecards and a run to start.

import { ApiError, type ApiClient } from "../../../shared/infrastructure/api-client";
import { DtoError } from "../../../shared/infrastructure/dto/dto-error";
import type { ScoreSource } from "../application/ports";
import { ScoreError, type ScoreErrorCode } from "../domain/bench";
import type { CaseDefinition, Expectation } from "../domain/bench";
import { BenchCaseDto, BenchCasesDto, BenchRunDto, CaseDefinitionRequestDto, ExpectationRequestDto, FieldErrorsDto, TrajectoriesDto, TrajectoryRequestDto } from "./dto/bench-dto";
import { ScorecardsDto } from "./dto/scorecard-dto";
import { toCase, toRun, toScorecard, toTrajectory } from "./score-mapper";

const enc = encodeURIComponent;
const NOT_FOUND = new Set(["PROJECT_NOT_FOUND", "BENCH_CASE_NOT_FOUND", "NOT_FOUND", "HTTP_404"]);

/** Every call fails with a ScoreError. */
async function mapErrors<T>(call: () => Promise<T>): Promise<T> {
  try {
    return await call();
  } catch (err) {
    if (err instanceof DtoError) throw new ScoreError("BAD_RESPONSE", err.message);
    if (err instanceof ApiError) {
      if (err.status === 422 && err.body !== undefined) {
        try {
          const refused = FieldErrorsDto.fromJSON(err.body).errors;
          throw new ScoreError("REFUSED", refused[0]?.message ?? err.message, refused);
        } catch (inner) {
          if (inner instanceof ScoreError) throw inner;
        }
      }
      const code: ScoreErrorCode = NOT_FOUND.has(err.code) ? "NOT_FOUND" : err.status === 503 ? "UNAVAILABLE" : err.code === "NETWORK_ERROR" || err.code === "BAD_RESPONSE" ? err.code : "UNKNOWN";
      throw new ScoreError(code, err.message);
    }
    throw err;
  }
}

const expectationRequest = (e: Expectation): ExpectationRequestDto => new ExpectationRequestDto({ tools: e.tools.map((t) => ({ tool: t.tool, params: t.params })), ordered: e.ordered, reference: e.reference });
const definitionRequest = (d: CaseDefinition): CaseDefinitionRequestDto => new CaseDefinitionRequestDto(d.name, d.prompt, d.mocks.map((t) => ({ name: t.name, description: t.description, result: t.result, is_error: t.isError })), expectationRequest(d.expect));
const oneCase = (raw: unknown) => toCase(BenchCaseDto.fromJSON(raw, ""));

export function httpScoreSource(api: ApiClient): ScoreSource {
  return {
    cases: (projectId, signal) => mapErrors(async () => BenchCasesDto.fromJSON(await api.get(`/projects/${enc(projectId)}/bench/cases`, undefined, signal)).cases.map(toCase)),
    scorecards: (projectId, signal) => mapErrors(async () => ScorecardsDto.fromJSON(await api.get(`/projects/${enc(projectId)}/bench/scorecards`, undefined, signal)).scorecards.map(toScorecard)),
    createCase: (projectId, definition) => mapErrors(async () => oneCase(await api.post(`/projects/${enc(projectId)}/bench/cases`, definitionRequest(definition)))),
    updateExpectation: (projectId, caseId, expect) => mapErrors(async () => oneCase(await api.patch(`/projects/${enc(projectId)}/bench/cases/${enc(caseId)}`, expectationRequest(expect)))),
    deleteCase: (projectId, caseId) => mapErrors(async () => void (await api.delete(`/projects/${enc(projectId)}/bench/cases/${enc(caseId)}`))),
    trajectories: (projectId, signal) => mapErrors(async () => TrajectoriesDto.fromJSON(await api.get(`/projects/${enc(projectId)}/bench/trajectories`, undefined, signal)).trajectories.map(toTrajectory)),
    caseFromTrajectory: (projectId, trajectoryId) => mapErrors(async () => oneCase(await api.post(`/projects/${enc(projectId)}/bench/cases/from-trajectory`, new TrajectoryRequestDto(trajectoryId)))),
    startRun: (projectId) => mapErrors(async () => toRun(BenchRunDto.fromJSON(await api.post(`/projects/${enc(projectId)}/bench/runs`)))),
  };
}
