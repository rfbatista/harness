// SandboxGateway over the harness HTTP API: every call answers the whole
// state, so the page never has to merge a change itself.

import { Codes, StructuredError } from "../../../shared/domain/errors.js";

/** @param {unknown} body @returns {import("../domain/ports.js").SandboxState} */
function toState(body) {
  const b = /** @type {any} */ (body);
  if (typeof b?.enabled !== "boolean" || !Array.isArray(b.paths)) {
    throw new StructuredError(Codes.BAD_RESPONSE, "Unexpected sandbox settings data.");
  }
  return {
    enabled: b.enabled,
    driver: typeof b.driver === "string" ? b.driver : "",
    paths: b.paths.map((/** @type {any} */ p) => ({ path: String(p.path), write: p.write === true })),
  };
}

/**
 * @param {import("../../../shared/infrastructure/api.js").ApiClient} api
 * @returns {import("../domain/ports.js").SandboxGateway}
 */
export function sandboxGateway(api) {
  return {
    get: async () => toState(await api.get("/get_sandbox_settings")),
    setEnabled: async (on) => toState(await api.post("/set_sandbox_enabled", { enabled: on })),
    addPath: async (path, write) => toState(await api.post("/add_sandbox_path", { path, write })),
    setPathAccess: async (path, write) => toState(await api.post("/update_sandbox_path", { path, write })),
    removePath: async (path) => toState(await api.post("/remove_sandbox_path", { path })),
  };
}
