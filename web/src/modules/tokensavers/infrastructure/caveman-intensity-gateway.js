// CavemanIntensityGateway over the harness HTTP API: the intensity is a
// global setting (domain.SettingCavemanIntensity).

import { Codes, StructuredError } from "../../../shared/domain/errors.js";

const KEY = "token_savers.caveman.intensity";

/**
 * @param {import("../../../shared/infrastructure/api.js").ApiClient} api
 * @returns {import("../domain/caveman-intensity-ports.js").CavemanIntensityGateway}
 */
export function cavemanIntensityGateway(api) {
  return {
    async set(intensity) {
      const body = await api.post("/update_settings", { settings: { [KEY]: intensity } });
      const stored = body?.settings?.[KEY];
      if (typeof stored !== "string") {
        throw new StructuredError(Codes.BAD_RESPONSE, "Unexpected settings data: no caveman intensity.");
      }
      return /** @type {import("../domain/caveman-intensity-ports.js").CavemanIntensity} */ (stored);
    },
  };
}
