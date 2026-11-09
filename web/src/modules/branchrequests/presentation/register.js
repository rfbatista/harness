// The branch requests module's Alpine components.

import { branchRequests } from "./components/branchRequests.js";

/**
 * @param {import("alpinejs").Alpine} Alpine
 * @param {{
 *   gateway: ReturnType<typeof import("../infrastructure/branch-requests-gateway.js").branchRequestsGateway>,
 *   clock: import("../../../shared/infrastructure/clock.js").Clock,
 * }} deps
 */
export function registerBranchRequests(Alpine, deps) {
  Alpine.data("branchRequests", branchRequests(deps));
}
