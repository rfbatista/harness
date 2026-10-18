// The Token savers page's Caveman intensity section (Alpine component).

import { cavemanIntensity } from "./components/cavemanIntensity.js";

/**
 * @param {import("alpinejs").Alpine} Alpine
 * @param {{ gateway: import("../domain/caveman-intensity-ports.js").CavemanIntensityGateway }} deps
 */
export function registerCavemanIntensity(Alpine, deps) {
  Alpine.data("cavemanIntensity", cavemanIntensity(deps));
}
