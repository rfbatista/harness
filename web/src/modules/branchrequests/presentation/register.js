// The branch requests module's Alpine components.

import { branchRequestLead } from "./components/branchRequestLead.js";
import { branchRequests } from "./components/branchRequests.js";

/**
 * @param {import("alpinejs").Alpine} Alpine
 * @param {{
 *   gateway: ReturnType<typeof import("../infrastructure/branch-requests-gateway.js").branchRequestsGateway>,
 *   clock: import("../../../shared/infrastructure/clock.js").Clock,
 * }} deps
 */
export function registerBranchRequests(Alpine, deps) {
  // The task page's pending requests by session, shared by the popover and the session rows' approvals text.
  const store = Alpine.reactive({ pendingBySession: {} });
  Alpine.data("branchRequests", branchRequests({ ...deps, store }));
  Alpine.data("branchRequestLead", branchRequestLead({ store }));
}
