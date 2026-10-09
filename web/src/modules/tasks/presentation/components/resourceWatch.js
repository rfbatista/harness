// The task toolbar's Resources N and Comments N links: counts kept live from
// the project feed. Seeded with the ids the server rendered, so the echo of
// an add counts once and a delete counts down.
//
//   <span x-data="tasksResourceWatch" data-project-id="p1" data-ticket-id="t1"
//         data-resource-ids="r1,r2" data-comment-ids="c1" style="display: contents">
//     <a …>Resources <span class="[ badge ]" x-text="resourceCount">2</span></a>
//     <a …>Comments <span class="[ badge ]" x-text="commentCount">1</span></a>
//   </span>

import { FeedStatus } from "../../../../shared/domain/feed.js";

const ids = (list) => (list ? list.split(",").filter(Boolean) : []);

/** @param {{ resources: import("../../domain/ports.js").ResourceGateway }} deps */
export const resourceWatch = ({ resources: gateway }) => () => {
  let unfollow = () => {};

  return {
    ticketId: "",
    resourceIds: [],
    commentIds: [],

    init() {
      const { projectId = "", ticketId = "", resourceIds = "", commentIds = "" } = this.$el.dataset;
      this.ticketId = ticketId;
      this.resourceIds = ids(resourceIds);
      this.commentIds = ids(commentIds);
      if (projectId && ticketId) unfollow = gateway.follow(projectId, (change) => this.apply(change), (status) => status === FeedStatus.RESYNCED && this.resync());
    },

    destroy() {
      unfollow();
    },

    get resourceCount() {
      return this.resourceIds.length;
    },
    get commentCount() {
      return this.commentIds.length;
    },

    /** Changes were missed while the feed was down: count again. */
    async resync() {
      try {
        const [resources, comments] = await Promise.all([gateway.listResources(this.ticketId), gateway.listComments(this.ticketId)]);
        this.resourceIds = resources.map((r) => r.id);
        this.commentIds = comments.map((c) => c.id);
      } catch {
        // keep the counts shown; the next resync asks again
      }
    },

    /** @param {import("../../domain/ports.js").ResourceChange} change */
    apply(change) {
      const add = (list, id) => (list.includes(id) ? list : [...list, id]);
      switch (change.kind) {
        case "resource":
          if (change.resource.ticketId === this.ticketId) this.resourceIds = add(this.resourceIds, change.resource.id);
          return;
        case "resource-deleted":
          if (change.ticketId === this.ticketId) this.resourceIds = this.resourceIds.filter((id) => id !== change.id);
          return;
        case "comment":
          if (change.comment.ticketId === this.ticketId) this.commentIds = add(this.commentIds, change.comment.id);
          return;
        case "comment-deleted":
          if (change.ticketId === this.ticketId) this.commentIds = this.commentIds.filter((id) => id !== change.id);
      }
    },
  };
};
