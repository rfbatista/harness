// What the attribution module needs from the outside world: the person's
// agent attribution (ADR 0063). Implemented by
// infrastructure/attribution-gateway.js over /api/*_agent_attribution.

/**
 * @typedef {"default" | "none" | "custom"} AttributionMode
 *
 * @typedef {object} AttributionChoice
 * @property {AttributionMode} mode
 * @property {string} text  The custom text; "" unless the mode is custom.
 *
 * @typedef {object} AttributionState
 * @property {AttributionChoice} commit
 * @property {AttributionChoice} pr
 * @property {string} preview  The JSON agents get, as the server builds it.
 *
 * @typedef {object} AttributionGateway
 * @property {() => Promise<AttributionState>} get
 * @property {(commit: AttributionChoice, pr: AttributionChoice) => Promise<AttributionState>} save
 *           Rejects with INVALID_INPUT when a custom text is empty.
 */

export {};
