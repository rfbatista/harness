// What the services module needs from the outside world. Implemented by
// infrastructure/services-gateway.js over the harness API (contract: Harness
// server ↔ Web UI — services).

/**
 * @typedef {object} ServicesSettings
 * @property {"docker" | "podman"} container_runtime
 * @property {boolean} available  whether the runtime's command line is found on the server
 */

/**
 * @typedef {object} Service  the API's service, mapped (infrastructure/dto.js); `run` keeps the API's field names
 * @property {string} id
 * @property {string} projectId
 * @property {string} name
 * @property {"command" | "image"} kind
 * @property {string} repositoryId
 * @property {boolean} exportRepositoryEnv
 * @property {string} env  as saved; shown only in the service form
 * @property {{ line: string, subdir: string, ports: number[] } | null} command
 * @property {{ ref: string, args: string[], ports: { host: number, container: number, protocol: "tcp" | "udp" }[] } | null} image
 * @property {"stopped" | "starting" | "running" | "exited" | "failed"} status
 * @property {object | null} run
 */

/**
 * @typedef {object} ServiceList
 * @property {Service[]} services
 * @property {number} seq  the services feed's sequence number when it was read
 * @property {{ name: "docker" | "podman", available: boolean }} runtime
 * @property {"on" | "off" | "unavailable"} sandbox
 */

/**
 * @typedef {object} ServiceEvent
 * @property {number} seq
 * @property {string} kind  service_changed | service_deleted
 * @property {Service | null} service
 * @property {string} serviceId
 * @property {number} runningCount
 */

/**
 * @typedef {object} ServicesGateway
 * @property {(seed: unknown) => { projectId: string, list: ServiceList | null }} decodeSeed
 * @property {(projectId: string) => Promise<ServiceList>} listServices
 * @property {(projectId: string, onEvent: (event: ServiceEvent) => void,
 *   onStatus?: (status: import("../../../shared/domain/feed.js").FeedStatus) => void) => () => void} follow
 *           Follows the project's services feed; returns the function that stops following.
 * @property {() => Promise<ServicesSettings>} getSettings
 * @property {(runtime: string) => Promise<ServicesSettings>} setSettings
 *           Rejects with INVALID_INPUT for any value but docker or podman.
 */

export {};
