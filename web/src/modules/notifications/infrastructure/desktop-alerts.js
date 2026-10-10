// DesktopAlerts over the browser's Notification API. A browser without it,
// or a page outside a secure context, reports "unsupported".

/**
 * @param {{ Notification?: typeof globalThis.Notification, document?: Document, window?: Window }} [env]
 * @returns {import("../domain/ports.js").DesktopAlerts}
 */
export function desktopAlerts({ Notification = globalThis.Notification, document = globalThis.document, window = globalThis.window } = {}) {
  const supported = typeof Notification === "function";
  return {
    permission: () => (supported ? Notification.permission : "unsupported"),
    async request() {
      if (!supported) return "unsupported";
      try {
        return await Notification.requestPermission();
      } catch {
        return Notification.permission;
      }
    },
    show({ title, body, tag, onClick }) {
      if (!supported || Notification.permission !== "granted") return;
      try {
        const alert = new Notification(title, { body, tag });
        alert.onclick = () => {
          window?.focus();
          alert.close();
          onClick();
        };
      } catch {
        // Some browsers only allow alerts from a service worker; the page goes on.
      }
    },
    hidden: () => document?.visibilityState === "hidden",
  };
}
