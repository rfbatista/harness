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
 *
 * @typedef {import("./modeAgents.js").ModeAgents} ModeAgents
 *
 * @typedef {object} ModeAgentsGateway
 * @property {() => Promise<ModeAgents[]>} getModeAgents  every session mode's agents, in the mode order (get_mode_agents)
 * @property {(write: { mode: string, state: string, agents: string[], noAgent: boolean }) => Promise<ModeAgents>} setModeAgents  replaces one mode's agents (set_mode_agents)
 * @property {() => Promise<{ name: string, description: string }[]>} getAgents  the catalog's agents
 * @property {(handlers: { onEvent: (data: unknown) => void, onStatus?: (status: string) => void }) => () => void} follow  the page feed (/api/project_events); returns a stop function
 */

export {};
