// The reviews module's Alpine components.

import { reviewsInbox } from "./components/reviewsInbox.js";

/**
 * @param {import("alpinejs").Alpine} Alpine
 * @param {{
 *   gateway: import("../domain/ports.js").ReviewGateway,
 *   clock: import("../../../shared/infrastructure/clock.js").Clock,
 * }} deps
 */
export function registerReviews(Alpine, deps) {
  Alpine.data("reviewsInbox", reviewsInbox(deps));
}
