// What the services module needs from the outside world. Implemented by
// infrastructure/services-gateway.js over the harness API (contract: Harness
// server ↔ Web UI — services).

/**
 * @typedef {object} ServicesSettings
 * @property {"docker" | "podman"} container_runtime
 * @property {boolean} available  whether the runtime's command line is found on the server
 */

/**
 * @typedef {object} ServicesGateway
 * @property {() => Promise<ServicesSettings>} getSettings
 * @property {(runtime: string) => Promise<ServicesSettings>} setSettings
 *           Rejects with INVALID_INPUT for any value but docker or podman.
 */

export {};
