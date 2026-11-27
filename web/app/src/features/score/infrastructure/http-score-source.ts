// ScoreSource over the bench API: cases, scorecards and a run to start.

import { ApiError, type ApiClient } from "../../../shared/infrastructure/api-client";
import { DtoError } from "../../../shared/infrastructure/dto/dto-error";
import type { ScoreSource } from "../application/ports";
import { ScoreError, type ScoreErrorCode } from "../domain/bench";
import { BenchCasesDto, BenchRunDto } from "./dto/bench-dto";
import { ScorecardsDto } from "./dto/scorecard-dto";
import { toCase, toRun, toScorecard } from "./score-mapper";

const enc = encodeURIComponent;
const NOT_FOUND = new Set(["PROJECT_NOT_FOUND", "NOT_FOUND", "HTTP_404"]);

/** Every call fails with a ScoreError. */
async function mapErrors<T>(call: () => Promise<T>): Promise<T> {
  try {
    return await call();
  } catch (err) {
    if (err instanceof DtoError) throw new ScoreError("BAD_RESPONSE", err.message);
    if (err instanceof ApiError) {
      const code: ScoreErrorCode = NOT_FOUND.has(err.code) ? "NOT_FOUND" : err.status === 503 ? "UNAVAILABLE" : err.code === "NETWORK_ERROR" || err.code === "BAD_RESPONSE" ? err.code : "UNKNOWN";
      throw new ScoreError(code, err.message);
    }
    throw err;
  }
}

export function httpScoreSource(api: ApiClient): ScoreSource {
  return {
    cases: (projectId, signal) => mapErrors(async () => BenchCasesDto.fromJSON(await api.get(`/projects/${enc(projectId)}/bench/cases`, undefined, signal)).cases.map(toCase)),
    scorecards: (projectId, signal) => mapErrors(async () => ScorecardsDto.fromJSON(await api.get(`/projects/${enc(projectId)}/bench/scorecards`, undefined, signal)).scorecards.map(toScorecard)),
    startRun: (projectId) => mapErrors(async () => toRun(BenchRunDto.fromJSON(await api.post(`/projects/${enc(projectId)}/bench/runs`)))),
  };
}
