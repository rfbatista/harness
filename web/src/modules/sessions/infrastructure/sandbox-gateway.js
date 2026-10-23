// SandboxGateway over the harness HTTP API: GET /api/sessions/{id}/sandbox/violations.

import { Codes, StructuredError } from "../../../shared/domain/errors.js";
import { optionalDate } from "./dto.js";

/** @returns {import("../domain/sandbox.js").Violation} */
export function toViolation(dto) {
  if (!dto || typeof dto.operation !== "string") bad("a violation without an operation");
  return Object.freeze({
    at: optionalDate(dto.when, "violation: when") ?? new Date(0),
    operation: dto.operation,
    path: typeof dto.path === "string" ? dto.path : "",
    process: typeof dto.process === "string" ? dto.process : "",
  });
}

/** @returns {import("../domain/sandbox.js").ViolationLog} */
export function toViolationLog(body) {
  if (!body || !Array.isArray(body.violations)) bad("expected {violations: [...]}");
  return Object.freeze({ available: body.available === true, violations: Object.freeze(body.violations.map(toViolation)) });
}

/**
 * @param {import("../../../shared/infrastructure/api.js").ApiClient} api
 * @returns {import("../domain/ports.js").SandboxGateway}
 */
export function sandboxGateway(api) {
  return {
    async violations(sessionId, signal) {
      return toViolationLog(await api.get(`/sessions/${encodeURIComponent(sessionId)}/sandbox/violations`, {}, signal));
    },
  };
}

function bad(detail) {
  throw new StructuredError(Codes.BAD_RESPONSE, `Unexpected sandbox data: ${detail}.`);
}
