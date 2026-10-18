// The wire format of token savers (internal/domain/token_saver.go).

import { Codes, StructuredError } from "../../../shared/domain/errors.js";

const STATES = ["not_installed", "installed", "outdated", "installing", "failed"];

/** @returns {import("../domain/installation.js").Saver} */
export function toSaver(dto) {
  if (!dto || typeof dto.saver !== "string" || dto.saver === "") bad("token saver without a name");
  if (!STATES.includes(dto.state)) bad(`token saver ${dto.saver} has unknown state "${dto.state}"`);
  return {
    saver: dto.saver,
    summary: dto.summary ?? "",
    pinnedVersion: dto.pinned_version ?? "",
    state: dto.state,
    installedVersion: dto.installed?.version ?? "",
    path: dto.installed?.path ?? "",
    progress: dto.progress ?? "",
    error: dto.error ? { code: dto.error.code ?? "", message: dto.error.message ?? "" } : null,
  };
}

/** list_token_savers' answer, which is also the page's seed. @returns {import("../domain/installation.js").SaverList} */
export function toSaverList(body) {
  if (!body || !Array.isArray(body.token_savers)) bad("expected {token_savers: [...]}");
  return {
    savers: body.token_savers.map(toSaver),
    platform: body.platform ?? "",
    supported: body.supported !== false,
    unsupported: body.unsupported ?? "",
  };
}

function bad(detail) {
  throw new StructuredError(Codes.BAD_RESPONSE, `Unexpected token saver data: ${detail}.`);
}
