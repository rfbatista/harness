// What the settings module needs from the outside world: one boolean global
// setting stored. Implemented by infrastructure/settings-gateway.js over the
// settings API.

/**
 * @typedef {object} SettingsGateway
 * @property {(key: string, on: boolean) => Promise<boolean>} setFlag
 *           Stores the flag under the settings key and answers it as stored.
 *           Rejects with INVALID_INPUT for a key or value the server does
 *           not take.
 */

export {};
