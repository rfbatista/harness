// The wire format of projects and repositories (internal/adapter/in/mcp
// ProjectDTO, internal/domain/repository.go) and its mapping.

import { Codes, StructuredError } from "../../../shared/domain/errors.js";

/** @returns {import("../domain/project.js").Project} */
export function toProject(dto) {
  if (!dto || typeof dto.id !== "string" || dto.id === "") bad("project without an id");
  return Object.freeze({ id: dto.id, name: dto.name ?? "", rootDir: dto.root_dir ?? "" });
}

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
