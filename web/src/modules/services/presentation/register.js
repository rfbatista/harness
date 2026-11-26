// The services module's Alpine components: the Services page and the container runtime pane on the Server page.

import { systemClock } from "../../../shared/infrastructure/clock.js";
import { serviceCredentials } from "./components/serviceCredentials.js";
import { serviceForm } from "./components/serviceForm.js";
import { serviceLog } from "./components/serviceLog.js";
import { serviceRequests } from "./components/serviceRequests.js";
import { serviceVariables } from "./components/serviceVariables.js";
import { servicesPage } from "./components/servicesPage.js";
import { servicesRuntime } from "./components/servicesRuntime.js";

/**
 * @param {import("alpinejs").Alpine} Alpine
 * @param {{ gateway: import("../domain/ports.js").ServicesGateway, prefs?: import("../../../shared/infrastructure/storage.js").Preferences }} deps
 */
export function registerServices(Alpine, deps) {
  Alpine.data("servicesPage", servicesPage({ gateway: deps.gateway, clock: deps.clock ?? systemClock, prefs: deps.prefs }));
  Alpine.data("serviceForm", serviceForm({ gateway: deps.gateway }));
  Alpine.data("serviceCredentials", serviceCredentials());
  Alpine.data("serviceLog", serviceLog({ gateway: deps.gateway, clock: deps.clock ?? systemClock }));
  Alpine.data("serviceRequests", serviceRequests({ gateway: deps.gateway }));
  Alpine.data("serviceVariables", serviceVariables({ gateway: deps.gateway }));
  Alpine.data("servicesRuntime", servicesRuntime(deps));
}
