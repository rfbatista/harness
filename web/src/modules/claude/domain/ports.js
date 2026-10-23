// What the managed claude module needs from the outside world: the claude CLI
// the harness installs for agents (ADR 0019). Implemented by
// infrastructure/claude-gateway.js over /api/*_managed_claude*.

/**
 * @typedef {object} ClaudeVersion
 * @property {string} version
 * @property {string} installedAt  ISO time.
 * @property {boolean} active  Agents use this one.
 *
 * @typedef {object} ClaudeInstall
 * @property {string} version
 * @property {number} done  Bytes downloaded.
 * @property {number} total  Bytes expected; -1 while unknown.
 *
 * @typedef {object} ClaudeState
 * @property {string} path  The stable path agents run.
 * @property {string} current  The active version; "" when none is installed.
 * @property {ClaudeVersion[]} installed
 * @property {ClaudeInstall | null} installing
 * @property {{ version: string, message: string } | null} failed
 * @property {boolean} overridden  -claude.bin / $CLAUDE_BIN decides.
 * @property {string} bin  The binary agents run.
 * @property {number} keepingSessions  In the answer to a switch: running sessions that keep the version they started with.
 *
 * @typedef {object} ClaudeReleases
 * @property {string} latest
 * @property {string} stable
 *
 * @typedef {object} ClaudeGateway
 * @property {() => Promise<ClaudeState>} get  Also the progress of an install.
 * @property {() => Promise<ClaudeReleases>} releases  Rejects when the release server cannot be reached.
 * @property {(version: string) => Promise<ClaudeState>} install  "latest", "stable" or X.Y.Z; starts the install.
 * @property {(version: string) => Promise<ClaudeState>} use  Switches agents to an installed version.
 * @property {() => Promise<ClaudeState>} cancel  Stops the install that runs; nothing it downloaded stays.
 * @property {(version: string) => Promise<ClaudeState>} remove  Deletes an installed version that is not the current one.
 */

export {};
