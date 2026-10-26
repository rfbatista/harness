// MergeGateway over the harness HTTP API: GET and POST /api/sessions/{id}/merge.

import { Codes, StructuredError } from "../../../shared/domain/errors.js";
import { toMergeResult } from "./dto.js";

/** @returns {import("../domain/ports.js").MergeTarget} */
function toTarget(dto) {
  return Object.freeze({ name: dto?.name ?? "", isDefault: dto?.default === true, checkedOut: dto?.checked_out === true, dirty: dto?.dirty === true });
}

/** @returns {import("../domain/ports.js").MergeOffer} */
export function toOffer(body) {
  if (!body || typeof body.available !== "boolean") {
    throw new StructuredError(Codes.BAD_RESPONSE, "Unexpected merge data: expected {available}.");
  }
  return Object.freeze({
    available: body.available,
    branch: body.branch ?? "",
    into: body.into ?? "",
    reason: body.reason ?? "",
    ahead: body.ahead ?? 0,
    uncommittedChanges: body.uncommitted_changes ?? 0,
    suggestedMessage: body.suggested_message ?? "",
    targets: Object.freeze((body.targets ?? []).map(toTarget)),
  });
}

/**
 * POST /api/sessions/{id}/merge body: what is not chosen is left out, so the
 * server applies its defaults (the default branch, a merge commit).
 */
export function toMergeBody({ target, method, message } = {}) {
  return {
    target: target || undefined,
    method: method || undefined,
    message: method === "squash" ? message : undefined,
  };
}

/**
 * @param {import("../../../shared/infrastructure/api.js").ApiClient} api
 * @returns {import("../domain/ports.js").MergeGateway}
 */
export function mergeGateway(api) {
  const path = (id) => `/sessions/${encodeURIComponent(id)}/merge`;
  return {
    async offer(sessionId, target, signal) {
      return toOffer(await api.get(path(sessionId), { target: target || undefined }, signal));
    },
    async merge(sessionId, request) {
      const result = toMergeResult(await api.post(path(sessionId), toMergeBody(request)));
      if (!result) throw new StructuredError(Codes.BAD_RESPONSE, "Unexpected merge data: expected a result.");
      return result;
    },
  };
}
