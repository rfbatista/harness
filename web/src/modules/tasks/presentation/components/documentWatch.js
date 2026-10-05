// Watches a task's documents for ones written after the page loaded: agents
// write them as they work. On the task's toolbar it keeps the documents
// count current; on the documents page it says the list changed and offers a
// reload (documents are rendered by the server). It asks every POLL_MS, and
// only while the tab is visible.
//
//   <a x-data="tasksDocumentWatch" data-ticket-id="t1" data-signature="…" data-count="2">
//     Documents <span x-text="count">2</span></a>

import { documentSignature } from "../../domain/documents.js";

export const POLL_MS = 10_000;

/**
 * @param {{
 *   gateway: import("../../domain/ports.js").TaskGateway,
 *   reload: () => void,
 *   setInterval?: typeof globalThis.setInterval,
 *   clearInterval?: typeof globalThis.clearInterval,
 *   isVisible?: () => boolean,
 * }} deps
 */
export const documentWatch = ({
  gateway,
  reload,
  setInterval = globalThis.setInterval.bind(globalThis),
  clearInterval = globalThis.clearInterval.bind(globalThis),
  isVisible = () => globalThis.document?.visibilityState !== "hidden",
}) => () => {
  let timer = null;
  let loaded = "";

  return {
    ticketId: "",
    count: 0,
    /** The documents differ from what the page was rendered with. */
    changed: false,

    init() {
      const { ticketId = "", signature = "", count = "0" } = this.$el.dataset;
      this.ticketId = ticketId;
      loaded = signature;
      this.count = Number(count) || 0;
      if (this.ticketId) timer = setInterval(() => isVisible() && this.check(), POLL_MS);
    },

    async check() {
      try {
        const versions = await gateway.listDocumentVersions(this.ticketId);
        this.count = versions.length;
        this.changed = documentSignature(versions) !== loaded;
      } catch {
        // keep what it shows; the next tick asks again
      }
    },

    reload() {
      reload();
    },

    destroy() {
      if (timer !== null) clearInterval(timer);
    },
  };
};
