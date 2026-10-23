// SettingsGateway over the harness HTTP API: a flag is a global setting
// holding "true" or "false".

import { Codes, StructuredError } from "../../../shared/domain/errors.js";

/**
 * @param {import("../../../shared/infrastructure/api.js").ApiClient} api
 * @returns {import("../domain/ports.js").SettingsGateway}
 */
export function settingsGateway(api) {
  return {
    async setFlag(key, on) {
      const body = await api.post("/update_settings", { settings: { [key]: String(on) } });
      const stored = body?.settings?.[key];
      if (stored !== "true" && stored !== "false") {
        throw new StructuredError(Codes.BAD_RESPONSE, "Unexpected settings data: no flag.");
      }
      return stored === "true";
    },
  };
}
