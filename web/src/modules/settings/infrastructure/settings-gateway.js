// SettingsGateway over the harness HTTP API: a flag is a global setting
// holding "true" or "false".

import { Codes, StructuredError } from "../../../shared/domain/errors.js";

const MEMORY_BUDGET_KEY = "memory.budget_bytes";

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
    async setMemoryBudget(bytes) {
      const body = await api.post("/update_settings", { settings: { [MEMORY_BUDGET_KEY]: String(bytes) } });
      const stored = Number(body?.settings?.[MEMORY_BUDGET_KEY]);
      if (!Number.isFinite(stored) || stored <= 0) {
        throw new StructuredError(Codes.BAD_RESPONSE, "Unexpected settings data: no memory budget.");
      }
      return stored;
    },
    async setMemoryBudgetOverride(level, ownerId, bytes) {
      const body = await api.post("/set_memory_budget_override", { level, owner_id: ownerId, bytes });
      if (typeof body?.bytes !== "number" || typeof body?.effective_bytes !== "number") {
        throw new StructuredError(Codes.BAD_RESPONSE, "Unexpected memory budget data.");
      }
      return { bytes: body.bytes, effectiveBytes: body.effective_bytes };
    },
    async setBranchBlock(key, on) {
      const body = await api.post("/update_settings", { settings: { [key]: on ? "on" : "off" } });
      const stored = body?.settings?.[key];
      if (stored !== "on" && stored !== "off") {
        throw new StructuredError(Codes.BAD_RESPONSE, "Unexpected settings data: no branch block.");
      }
      return stored === "on";
    },
  };
}
