// The wire format of projects and repositories (internal/adapter/in/mcp
// ProjectDTO, internal/domain/repository.go) and its mapping.

import { Codes, StructuredError } from "../../../shared/domain/errors.js";

/** @returns {import("../domain/project.js").Project} */
export function toProject(dto) {
  if (!dto || typeof dto.id !== "string" || dto.id === "") bad("project without an id");
  const ignored = Array.isArray(dto.ignored_paths) ? dto.ignored_paths.filter((p) => typeof p === "string") : [];
  return Object.freeze({ id: dto.id, name: dto.name ?? "", rootDir: dto.root_dir ?? "", ignoredPaths: Object.freeze(ignored) });
}

/** One of GET /api/list_project_summaries' summaries. @returns {import("../domain/project.js").ProjectSummary} */
export function toProjectSummary(dto) {
  const at = dto?.last_activity_at ? new Date(dto.last_activity_at) : null;
  return Object.freeze({
    project: toProject(dto?.project),
    repositoryCount: count(dto.repository_count),
    openTaskCount: count(dto.open_task_count),
    runningSessionCount: count(dto.running_session_count),
    lastActivityAt: at && !Number.isNaN(at.getTime()) ? at : null,
  });
}

/** GET /api/list_project_summaries, and the projects page seed: {"summaries": [...]} */
export function toProjectsSeed(body) {
  if (!body || !Array.isArray(body.summaries)) bad("expected {summaries: [...]}");
  return body.summaries.map(toProjectSummary);
}

/** The settings page seed: {"project": {...}, "repositories": [...]} */
export function toSettingsSeed(body) {
  if (!body || !Array.isArray(body.repositories)) bad("expected {project, repositories: [...]}");
  return { project: toProject(body.project), repositories: body.repositories.map(toRepository) };
}

/**
 * A /api/project_events message: the project as it is now, or its id with
 * deleted. Null for a message that is neither (clients ignore what they do
 * not know).
 * @returns {{ kind: "upsert", project: import("../domain/project.js").Project } | { kind: "deleted", id: string } | null}
 */
export function toCatalogChange(dto) {
  if (typeof dto?.project?.id !== "string" || dto.project.id === "") return null;
  if (dto.deleted === true) return { kind: "deleted", id: dto.project.id };
  return { kind: "upsert", project: toProject(dto.project) };
}

/** PROJECT_HAS_RUNNING_SESSIONS details: {"sessions": [{id, ticket_id, agent}]} */
export function toRunningSessions(details) {
  const list = Array.isArray(details?.sessions) ? details.sessions : [];
  return list
    .filter((s) => typeof s?.id === "string" && s.id !== "")
    .map((s) => Object.freeze({ id: s.id, ticketId: s.ticket_id ?? "", agent: s.agent ?? "" }));
}

const count = (n) => (Number.isInteger(n) && n > 0 ? n : 0);

/** @returns {import("../domain/project.js").Repository} */
export function toRepository(dto) {
  if (!dto || typeof dto.id !== "string" || dto.id === "") bad("repository without an id");
  return Object.freeze({
    id: dto.id,
    projectId: dto.project_id ?? "",
    name: dto.name ?? "",
    description: dto.description ?? "",
    url: dto.url ?? "",
    rootDir: dto.root_dir ?? "",
  });
}

/** The repositories page seed: {"project_id", "project_root", "repositories": [...]} */
export function toRepositoriesSeed(body) {
  if (!body || typeof body.project_id !== "string" || !Array.isArray(body.repositories)) {
    bad("expected {project_id, repositories: [...]}");
  }
  return { projectId: body.project_id, projectRoot: body.project_root ?? "", repositories: body.repositories.map(toRepository) };
}

/** GET /api/find_repositories → {"root": "...", "repositories": [{path, name, remote}]} */
export function toFound(body) {
  if (!body || typeof body.root !== "string" || !Array.isArray(body.repositories)) bad("expected {root, repositories: [...]}");
  return {
    root: body.root,
    found: body.repositories.map((r) => {
      if (typeof r?.path !== "string" || r.path === "") bad("found repository without a path");
      return Object.freeze({ path: r.path, name: r.name ?? "", remote: r.remote ?? "" });
    }),
  };
}

/** @returns {import("../domain/project.js").EnvFile} */
export function toEnvFile(dto) {
  if (!dto || typeof dto.path !== "string" || dto.path === "") bad("env file without a path");
  const updated = dto.updated_at ? new Date(dto.updated_at) : null;
  return Object.freeze({
    path: dto.path,
    content: dto.content ?? "",
    updatedAt: updated && !Number.isNaN(updated.getTime()) ? updated : null,
  });
}

/** The env files page seed: {"repository_id": "...", "env_files": [...]} */
export function toEnvFilesSeed(body) {
  if (!body || typeof body.repository_id !== "string" || !Array.isArray(body.env_files)) {
    bad("expected {repository_id, env_files: [...]}");
  }
  return { repositoryId: body.repository_id, files: body.env_files.map(toEnvFile) };
}

function bad(detail) {
  throw new StructuredError(Codes.BAD_RESPONSE, `Unexpected project data: ${detail}.`);
}
