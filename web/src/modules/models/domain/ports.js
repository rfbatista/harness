// What the models module needs from the outside world: the stored model of
// each session tier. Implemented by infrastructure/models-gateway.js over
// the settings API (domain.SettingModelArchitect and
// domain.SettingModelImplementation).

/**
 * @typedef {"default" | "fable" | "opus" | "sonnet" | "haiku"} ModelChoice
 *
 * @typedef {object} ModelGateway
 * @property {(key: string, model: ModelChoice) => Promise<ModelChoice>} setModel
 *           Stores the model of the tier the settings key names and answers
 *           it as stored. Rejects with INVALID_INPUT for a key or model the
 *           server does not take.
 */

export {};
