// The wire format of repository indexes (internal/domain/repository_index.go).

import { Codes, StructuredError } from "../../../shared/domain/errors.js";

/** @returns {import("../domain/repository-index.js").RepositoryIndex} */
export function toRepositoryIndex(dto) {
  if (!dto || typeof dto.repository_id !== "string" || dto.repository_id === "") bad("repository index without a repository");
  const builtAt = dto.built_at ? new Date(dto.built_at) : null;
  if (builtAt && Number.isNaN(builtAt.getTime())) bad(`repository index ${dto.repository_id} has a bad built_at "${dto.built_at}"`);
  return {
    repositoryId: dto.repository_id,
    repositoryName: dto.repository_name ?? "",
    projectName: dto.project_name ?? "",
    checkout: dto.checkout ?? "",
    builtAt,
    building: dto.building === true,
    error: dto.error ?? "",
  };
}

/** list_repository_indexes' answer, which is also the section's seed. */
export function toRepositoryIndexes(body) {
  if (!body || !Array.isArray(body.repository_indexes)) bad("expected {repository_indexes: [...]}");
  return body.repository_indexes.map(toRepositoryIndex);
}

function bad(detail) {
  throw new StructuredError(Codes.BAD_RESPONSE, `Unexpected repository index data: ${detail}.`);
}
