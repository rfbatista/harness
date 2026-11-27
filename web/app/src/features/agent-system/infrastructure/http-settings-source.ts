// SettingsSource over the agent-system API (GET and PATCH, for the project and for a repository) and the repositories list.

import { ApiError, type ApiClient } from "../../../shared/infrastructure/api-client";
import { DtoError } from "../../../shared/infrastructure/dto/dto-error";
import type { SettingsSource } from "../application/ports";
import { SettingsError, SettingsRefused, type Scope, type SettingsErrorCode } from "../domain/settings";
import { PatchDto, RefusalDto, RepositoriesDto, RunCommandsDto, ScorecardsDto, SettingsViewDto } from "./dto/settings-view-dto";
import { toSettingsView } from "./settings-mapper";

const enc = encodeURIComponent;

const NOT_FOUND = new Set(["PROJECT_NOT_FOUND", "REPOSITORY_NOT_FOUND", "NOT_FOUND", "HTTP_404"]);

/** Every call fails with a SettingsError, or a SettingsRefused for a 422. */
async function mapErrors<T>(call: () => Promise<T>): Promise<T> {
  try {
    return await call();
  } catch (err) {
    if (err instanceof DtoError) throw new SettingsError("BAD_RESPONSE", err.message);
    if (err instanceof ApiError) {
      if (err.status === 422 && err.body !== undefined) {
        try {
          throw new SettingsRefused(RefusalDto.fromJSON(err.body).errors);
        } catch (inner) {
          if (inner instanceof SettingsRefused) throw inner;
          throw new SettingsError("BAD_RESPONSE", err.message);
        }
      }
      const code: SettingsErrorCode = NOT_FOUND.has(err.code) ? "NOT_FOUND" : err.code === "NETWORK_ERROR" || err.code === "BAD_RESPONSE" ? err.code : "UNKNOWN";
      throw new SettingsError(code, err.message);
    }
    throw err;
  }
}

const path = (projectId: string, scope: Scope): string => `/projects/${enc(projectId)}${scope.kind === "repository" ? `/repositories/${enc(scope.id)}` : ""}/agent-system`;

export function httpSettingsSource(api: ApiClient): SettingsSource {
  return {
    repositories: (projectId, signal) => mapErrors(async () => [...RepositoriesDto.fromJSON(await api.get("/list_repositories", { project_id: projectId }, signal)).repositories]),
    view: (projectId, scope, signal) => mapErrors(async () => toSettingsView(SettingsViewDto.fromJSON(await api.get(path(projectId, scope), undefined, signal)))),
    runCommands: (repositoryId, signal) => mapErrors(async () => [...RunCommandsDto.fromJSON(await api.get("/list_repository_run_commands", { repository_id: repositoryId }, signal)).run_commands]),
    lastScore: (projectId, signal) =>
      mapErrors(async () => {
        const card = ScorecardsDto.fromJSON(await api.get(`/projects/${enc(projectId)}/bench/scorecards`, undefined, signal)).scorecards.find((c) => c.status === "finished");
        return card === undefined ? undefined : { mean: card.mean, costUsd: card.cost_usd, cases: card.cases };
      }),
    save: (projectId, scope, patch) => mapErrors(async () => toSettingsView(SettingsViewDto.fromJSON(await api.patch(path(projectId, scope), new PatchDto(patch))))),
  };
}
