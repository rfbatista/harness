// The notifications module's Alpine components.

import { inboxFilters } from "./components/inboxFilters.js";
import { notificationsPage } from "./components/notificationsPage.js";
import { pageSeed } from "./page-seed.js";
import { notificationsStore } from "./store.js";

/**
 * @param {import("alpinejs").Alpine} Alpine
 * @param {{
 *   gateway: import("../domain/ports.js").NotificationGateway,
 *   alerts: import("../domain/ports.js").DesktopAlerts,
 *   prefs: import("../../../shared/infrastructure/storage.js").Preferences,
 *   clock: import("../../../shared/infrastructure/clock.js").Clock,
 *   navigate: (url: string) => void,
 * }} deps
 */
export function registerNotifications(Alpine, deps) {
  // What is new is said in the stream bar's polite region (streamAnnouncer).
  const announce = (text) => window.dispatchEvent(new CustomEvent("rail-announce", { detail: text }));
  Alpine.store("notifications", notificationsStore({ ...deps, announce }));
  const store = Alpine.store("notifications");
  // The store follows the inbox on every page, for the title's count and what is said in the stream
  // bar: the top bar is a React island now, so it is not the bell that starts it.
  store.start(pageSeed(document));
  Alpine.data("inboxFilters", inboxFilters());
  Alpine.data("notificationsPage", notificationsPage({ ...deps, store }));
}
