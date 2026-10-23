// ClaudeGateway over the harness HTTP API: every call but releases answers
// the whole state, so the page never merges a change itself.

import { Codes, StructuredError } from "../../../shared/domain/errors.js";

/** @param {unknown} body @returns {import("../domain/ports.js").ClaudeState} */
function toState(body) {
  const b = /** @type {any} */ (body);
  if (typeof b?.current !== "string" || !Array.isArray(b.installed)) {
    throw new StructuredError(Codes.BAD_RESPONSE, "Unexpected managed claude data.");
  }
  return {
    path: String(b.path ?? ""),
    current: b.current,
    installed: b.installed.map((/** @type {any} */ v) => ({
      version: String(v.version),
      installedAt: String(v.installedAt ?? ""),
      active: v.active === true,
    })),
    installing: b.installing ? { version: String(b.installing.version), done: Number(b.installing.done), total: Number(b.installing.total) } : null,
    failed: b.failed ? { version: String(b.failed.version), message: String(b.failed.message) } : null,
    overridden: b.overridden === true,
    bin: String(b.bin ?? ""),
    keepingSessions: Number(b.keepingSessions ?? 0),
  };
}

/**
 * @param {import("../../../shared/infrastructure/api.js").ApiClient} api
 * @returns {import("../domain/ports.js").ClaudeGateway}
 */
export function claudeGateway(api) {
  return {
    get: async () => toState(await api.get("/get_managed_claude")),
    releases: async () => {
      const b = await api.get("/list_managed_claude_releases");
      if (typeof b?.latest !== "string") throw new StructuredError(Codes.BAD_RESPONSE, "Unexpected release data.");
      return { latest: b.latest, stable: typeof b.stable === "string" ? b.stable : b.latest };
    },
    install: async (version) => toState(await api.post("/install_managed_claude", { version })),
    use: async (version) => toState(await api.post("/switch_managed_claude", { version })),
    cancel: async () => toState(await api.post("/cancel_managed_claude_install", {})),
    remove: async (version) => toState(await api.post("/remove_managed_claude_version", { version })),
  };
}
