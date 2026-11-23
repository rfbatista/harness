// ModelGateway over the harness HTTP API: each tier's model is a global
// setting.

import { Codes, StructuredError } from "../../../shared/domain/errors.js";

/**
 * @param {import("../../../shared/infrastructure/api.js").ApiClient} api
 * @param {import("../../../shared/infrastructure/feed.js").Feed} [events]  the page feed, /api/project_events
 * @returns {import("../domain/ports.js").ModelGateway & import("../domain/ports.js").ModeAgentsGateway}
 */
export function modelsGateway(api, events) {
  return {
    follow({ onEvent, onStatus = () => {} }) {
      if (!events) return () => {};
      return events.follow("/project_events", { onMessage: onEvent, onStatus });
    },
    async getModeAgents() {
      const body = await api.get("/get_mode_agents");
      if (!Array.isArray(body?.modes)) throw new StructuredError(Codes.BAD_RESPONSE, "Unexpected mode agents data: no modes.");
      return body.modes.map(toModeAgents);
    },
    async setModeAgents({ mode, state, agents, noAgent }) {
      return toModeAgents(await api.post("/set_mode_agents", { mode, state, agents, no_agent: noAgent }));
    },
    async getAgents() {
      const body = await api.get("/get_agent_catalog");
      return (Array.isArray(body?.agents) ? body.agents : []).map((a) => ({ name: String(a?.name ?? ""), description: String(a?.description ?? "") }));
    },
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

/** @param {any} e one entry of the mode agents read @returns {import("../domain/modeAgents.js").ModeAgents} */
function toModeAgents(e) {
  return {
    mode: String(e?.mode ?? ""),
    name: String(e?.name ?? ""),
    state: e?.state === "selected" ? "selected" : "all",
    agents: Array.isArray(e?.agents) ? e.agents.map(String) : [],
    noAgent: e?.no_agent !== false,
  };
}
