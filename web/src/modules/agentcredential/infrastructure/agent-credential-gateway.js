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

const PHASES = ["idle", "starting", "waiting_for_code", "checking", "done", "failed"];

/** @param {unknown} body @returns {import("../domain/ports.js").OAuthState} */
function toOAuth(body) {
  const b = /** @type {any} */ (body);
  if (!PHASES.includes(b?.phase)) {
    throw new StructuredError(Codes.BAD_RESPONSE, "Unexpected sign-in data.");
  }
  return {
    phase: b.phase,
    url: typeof b.url === "string" ? b.url : "",
    remaining_seconds: Number.isFinite(b.remaining_seconds) ? b.remaining_seconds : 0,
    message: typeof b.message === "string" ? b.message : "",
    detail: typeof b.detail === "string" ? b.detail : "",
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
    oauthState: async () => toOAuth(await api.get("/get_agent_oauth")),
    oauthStart: async () => toOAuth(await api.post("/start_agent_oauth", {})),
    oauthSubmit: async (code) => toOAuth(await api.post("/submit_agent_oauth_code", { code })),
    oauthCancel: async () => toOAuth(await api.post("/cancel_agent_oauth", {})),
  };
}
