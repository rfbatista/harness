// ModelGateway over the harness HTTP API: each tier's model is a global
// setting.

import { Codes, StructuredError } from "../../../shared/domain/errors.js";

/**
 * @param {import("../../../shared/infrastructure/api.js").ApiClient} api
 * @returns {import("../domain/ports.js").ModelGateway}
 */
export function modelsGateway(api) {
  return {
    async setModel(key, model) {
      const body = await api.post("/update_settings", { settings: { [key]: model } });
      const stored = body?.settings?.[key];
      if (typeof stored !== "string") {
        throw new StructuredError(Codes.BAD_RESPONSE, "Unexpected settings data: no model.");
      }
      return /** @type {import("../domain/ports.js").ModelChoice} */ (stored);
    },
  };
}
