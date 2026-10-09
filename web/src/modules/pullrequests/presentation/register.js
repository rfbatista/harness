// The pull requests module's Alpine components. The badges and the task
// page's watch share one reactive store.

import { pullRequestBadge } from "./components/pullRequestBadge.js";
import { pullRequestPanel } from "./components/pullRequestPanel.js";
import { emptyStore, pullRequestWatch } from "./components/pullRequestWatch.js";

/**
 * @param {import("alpinejs").Alpine} Alpine
 * @param {{ gateway: import("../domain/ports.js").PullRequestGateway }} deps
 */
export function registerPullRequests(Alpine, { gateway }) {
  const store = Alpine.reactive(emptyStore());
  Alpine.data("pullRequestPanel", pullRequestPanel({ gateway }));
  Alpine.data("pullRequestWatch", pullRequestWatch({ gateway, store }));
  Alpine.data("pullRequestBadge", pullRequestBadge({ store }));
}
