// The Services item's running count in the project navigation, on every
// project page: the server renders it (data-running-services), and the
// project's services feed keeps it current with each event's running_count
// (contract: Harness server ↔ Web UI — services, sections 6 and 8).
//
//   <li data-page="services" x-data="servicesNavCount" data-project-id="p1" data-running-services="3">

import { FeedStatus } from "../../../../shared/domain/feed.js";
import { navBadge, runningCount } from "../../domain/service.js";

/** @param {{ gateway: import("../../domain/ports.js").ServicesGateway }} deps */
export const servicesNavCount = ({ gateway }) => () => {
  let unfollow = () => {};
  /** The item itself, kept from init: it carries data-running-services. */
  let root = null;

  return {
    count: 0,
    /** The last feed sequence number this count is current to. */
    seq: 0,

    get badgeText() {
      return navBadge(this.count).text;
    },
    get badgeShown() {
      return !navBadge(this.count).hidden;
    },

    init() {
      root = this.$el;
      const projectId = this.$el.dataset.projectId ?? "";
      this.count = Number(this.$el.dataset.runningServices ?? 0) || 0;
      if (projectId === "") return;
      unfollow = gateway.follow(
        projectId,
        (event) => {
          if (event.seq <= this.seq) return;
          this.seq = event.seq;
          this.show(event.runningCount);
        },
        (status) => {
          if (status === FeedStatus.RESYNCED) this.reread(projectId);
        },
      );
    },

    /** The count, on the badge and on the item's data-running-services. */
    show(count) {
      this.count = count;
      if (root) root.dataset.runningServices = String(count);
    },

    destroy() {
      unfollow();
    },

    /** Events were missed: the list says how many run now, and the sequence it is current to. */
    async reread(projectId) {
      try {
        const list = await gateway.listServices(projectId);
        this.seq = Math.max(this.seq, list.seq);
        this.show(runningCount(list.services));
      } catch {
        // the next event or resync corrects it
      }
    },
  };
};
