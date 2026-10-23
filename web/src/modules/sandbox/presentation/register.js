// The sandbox module's Alpine components: the Sandbox section of the Agent
// isolation page.

import { sandboxSettings } from "./components/sandboxSettings.js";

/**
 * @param {import("alpinejs").Alpine} Alpine
 * @param {{ gateway: import("../domain/ports.js").SandboxGateway }} deps
 */
export function registerSandbox(Alpine, deps) {
  Alpine.data("sandboxSettings", sandboxSettings(deps));
}
