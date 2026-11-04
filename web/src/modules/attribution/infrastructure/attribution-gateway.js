// AttributionGateway over the harness HTTP API: every call answers the whole
// state. The server stores the default mode as "" and the page names it
// "default"; this is where the two meet.

import { Codes, StructuredError } from "../../../shared/domain/errors.js";

/** @param {any} c @returns {import("../domain/ports.js").AttributionChoice} */
function toChoice(c) {
  const mode = c?.mode === "none" || c?.mode === "custom" ? c.mode : "default";
  return { mode, text: mode === "custom" ? String(c.text ?? "") : "" };
}

/** @param {import("../domain/ports.js").AttributionChoice} c */
function toWire(c) {
  return { mode: c.mode === "default" ? "" : c.mode, text: c.mode === "custom" ? c.text : "" };
}

/** @param {unknown} body @returns {import("../domain/ports.js").AttributionState} */
export function toState(body) {
  const b = /** @type {any} */ (body);
  if (typeof b?.preview !== "string") {
    throw new StructuredError(Codes.BAD_RESPONSE, "Unexpected agent attribution data.");
  }
  return { commit: toChoice(b.commit), pr: toChoice(b.pr), preview: b.preview };
}

/**
 * @param {import("../../../shared/infrastructure/api.js").ApiClient} api
 * @returns {import("../domain/ports.js").AttributionGateway}
 */
export function attributionGateway(api) {
  return {
    get: async () => toState(await api.get("/get_agent_attribution")),
    save: async (commit, pr) => toState(await api.post("/set_agent_attribution", { commit: toWire(commit), pr: toWire(pr) })),
  };
}
