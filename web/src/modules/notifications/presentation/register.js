// The notifications module's Alpine components.

import { notificationsBell } from "./components/notificationsBell.js";
import { notificationsPage } from "./components/notificationsPage.js";
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
  Alpine.data("notificationsBell", notificationsBell({ ...deps, store }));
  Alpine.data("notificationsPage", notificationsPage({ ...deps, store }));
}
