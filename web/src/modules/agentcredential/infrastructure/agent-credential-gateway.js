// AgentCredentialGateway over the harness HTTP API: every call answers the
// whole status, never the value.

import { Codes, StructuredError } from "../../../shared/domain/errors.js";

/** @param {unknown} body @returns {import("../domain/ports.js").CredentialStatus} */
function toStatus(body) {
  const b = /** @type {any} */ (body);
  if (typeof b?.set !== "boolean") {
    throw new StructuredError(Codes.BAD_RESPONSE, "Unexpected agent credential data.");
  }
  return {
    set: b.set,
    kind: b.set && (b.kind === "oauth" || b.kind === "api_key") ? b.kind : "",
    updated: b.set && typeof b.updated === "string" ? b.updated : "",
  };
}

/**
 * @param {import("../../../shared/infrastructure/api.js").ApiClient} api
 * @returns {import("../domain/ports.js").AgentCredentialGateway}
 */
export function agentCredentialGateway(api) {
  return {
    get: async () => toStatus(await api.get("/get_agent_credential")),
    set: async (kind, value) => toStatus(await api.post("/set_agent_credential", { kind, value })),
    remove: async () => toStatus(await api.post("/remove_agent_credential", {})),
  };
}
