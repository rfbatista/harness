// The services module's Alpine components: the container runtime pane on the Server page.

import { servicesRuntime } from "./components/servicesRuntime.js";

/**
 * @param {import("alpinejs").Alpine} Alpine
 * @param {{ gateway: import("../domain/ports.js").ServicesGateway }} deps
 */
export function registerServices(Alpine, deps) {
  Alpine.data("servicesRuntime", servicesRuntime(deps));
}
