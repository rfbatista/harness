// The services module's Alpine components: the Services page and the container runtime pane on the Server page.

import { systemClock } from "../../../shared/infrastructure/clock.js";
import { servicesPage } from "./components/servicesPage.js";
import { servicesRuntime } from "./components/servicesRuntime.js";

/**
 * @param {import("alpinejs").Alpine} Alpine
 * @param {{ gateway: import("../domain/ports.js").ServicesGateway }} deps
 */
export function registerServices(Alpine, deps) {
  Alpine.data("servicesPage", servicesPage({ gateway: deps.gateway, clock: deps.clock ?? systemClock }));
  Alpine.data("servicesRuntime", servicesRuntime(deps));
}
