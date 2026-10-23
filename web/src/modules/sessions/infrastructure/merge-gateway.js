// MergeGateway over the harness HTTP API: GET and POST /api/sessions/{id}/merge.

import { Codes, StructuredError } from "../../../shared/domain/errors.js";
import { toMergeResult } from "./dto.js";

/** @returns {import("../domain/ports.js").MergeOffer} */
export function toOffer(body) {
  if (!body || typeof body.available !== "boolean") {
    throw new StructuredError(Codes.BAD_RESPONSE, "Unexpected merge data: expected {available}.");
  }
  return Object.freeze({ available: body.available, into: body.into ?? "", reason: body.reason ?? "" });
}

/**
 * @param {import("../../../shared/infrastructure/api.js").ApiClient} api
 * @returns {import("../domain/ports.js").MergeGateway}
 */
export function mergeGateway(api) {
  const path = (id) => `/sessions/${encodeURIComponent(id)}/merge`;
  return {
    async offer(sessionId, signal) {
      return toOffer(await api.get(path(sessionId), {}, signal));
    },
    async merge(sessionId) {
      const result = toMergeResult(await api.post(path(sessionId)));
      if (!result) throw new StructuredError(Codes.BAD_RESPONSE, "Unexpected merge data: expected a result.");
      return result;
    },
  };
}
