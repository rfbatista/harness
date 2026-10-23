// What the sandbox module needs from the outside world: the person's agent
// sandbox configuration (ADR 0017). Implemented by
// infrastructure/sandbox-gateway.js over /api/*_sandbox_*.

/**
 * @typedef {object} SandboxPath
 * @property {string} path  As the person wrote it: absolute, or starting with ~.
 * @property {boolean} write  Read and write; false is read-only.
 *
 * @typedef {object} SandboxState
 * @property {boolean} enabled
 * @property {string} driver  The mechanism confining agents; "" when the platform has none.
 * @property {SandboxPath[]} paths
 *
 * @typedef {object} SandboxGateway
 * @property {() => Promise<SandboxState>} get
 * @property {(on: boolean) => Promise<SandboxState>} setEnabled
 * @property {(path: string, write: boolean) => Promise<SandboxState>} addPath
 *           Rejects with INVALID_INPUT, whose message says why the path is refused.
 * @property {(path: string, write: boolean) => Promise<SandboxState>} setPathAccess
 * @property {(path: string) => Promise<SandboxState>} removePath
 */

export {};
