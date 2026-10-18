// The wire format of token savers: activations (internal/domain/tokensaver.go),
// installations (internal/domain/token_saver_installation.go) and
// repository indexes (internal/domain/repository_index.go).

import { Codes, StructuredError } from "../../../shared/domain/errors.js";

const VALUES = ["inherit", "on", "off"];
const STATES = ["not_installed", "installed", "outdated", "installing", "failed"];

/** @returns {import("../domain/ports.js").Activations} */
export function toActivations(body) {
  if (!body || typeof body.level !== "string" || typeof body.owner_id !== "string" || !Array.isArray(body.activations)) {
    bad("token saver", "expected {level, owner_id, activations: [...]}");
  }
  return { level: body.level, ownerId: body.owner_id, activations: body.activations.map(toActivation) };
}

function toActivation(dto) {
  if (!dto || typeof dto.saver !== "string" || dto.saver === "") bad("token saver", "activation without a saver");
  if (!VALUES.includes(dto.value)) bad("token saver", `${dto.saver} has unknown value "${dto.value}"`);
  return Object.freeze({ saver: dto.saver, value: dto.value, summary: dto.summary ?? "" });
}

/** @returns {import("../domain/installation.js").Saver} */
export function toSaver(dto) {
  if (!dto || typeof dto.saver !== "string" || dto.saver === "") bad("token saver", "token saver without a name");
  if (!STATES.includes(dto.state)) bad("token saver", `token saver ${dto.saver} has unknown state "${dto.state}"`);
  return {
    saver: dto.saver,
    summary: dto.summary ?? "",
    pinnedVersion: dto.pinned_version ?? "",
    state: dto.state,
    installedVersion: dto.installed?.version ?? "",
    path: dto.installed?.path ?? "",
    progress: dto.progress ?? "",
    error: dto.error ? { code: dto.error.code ?? "", message: dto.error.message ?? "" } : null,
    onSomewhere: dto.on_somewhere === true,
  };
}

/** list_token_savers' answer, which is also the page's seed. @returns {import("../domain/installation.js").SaverList} */
export function toSaverList(body) {
  if (!body || !Array.isArray(body.token_savers)) bad("token saver", "expected {token_savers: [...]}");
  return {
    savers: body.token_savers.map(toSaver),
    platform: body.platform ?? "",
    supported: body.supported !== false,
    unsupported: body.unsupported ?? "",
  };
}

/** @returns {import("../domain/repository-index.js").RepositoryIndex} */
export function toRepositoryIndex(dto) {
  if (!dto || typeof dto.repository_id !== "string" || dto.repository_id === "") bad("repository index", "repository index without a repository");
  const builtAt = dto.built_at ? new Date(dto.built_at) : null;
  if (builtAt && Number.isNaN(builtAt.getTime())) bad("repository index", `repository index ${dto.repository_id} has a bad built_at "${dto.built_at}"`);
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
  if (!body || !Array.isArray(body.repository_indexes)) bad("repository index", "expected {repository_indexes: [...]}");
  return body.repository_indexes.map(toRepositoryIndex);
}

function bad(what, detail) {
  throw new StructuredError(Codes.BAD_RESPONSE, `Unexpected ${what} data: ${detail}.`);
}
