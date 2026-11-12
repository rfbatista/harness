// The service requests agents wait on the person for (ADR 0083): the "Requests N"
// badge in the Services page toolbar, counted from the project's pending requests
// and kept live by the services feed (contract: Harness server ↔ Web UI — services,
// section 13). A request is kept here without its variable values: they are read
// with the list call when the person opens the requests, never from the feed.
//
//   <span x-data="serviceRequests" data-seed="services-seed"> … </span>

import { FeedStatus } from "../../../../shared/domain/feed.js";
import { describeError } from "../../../../shared/presentation/errors.js";
import { readSeed } from "../../../../shared/presentation/seed.js";
import { pendingCount, requestsLabel, stripValues, upsertRequest } from "../../domain/request.js";

/** @param {{ gateway: import("../../domain/ports.js").ServicesGateway }} deps */
export const serviceRequests = ({ gateway }) => () => {
  let unfollow = () => {};

  return {
    projectId: "",
    /** @type {import("../../domain/ports.js").ServiceRequest[]} without values */
    requests: [],
    loaded: false,
    error: null,

    // ── what the markup binds ────────────────────────────────────────────
    get pendingCount() {
      return pendingCount(this.requests);
    },
    get shown() {
      return this.error === null && this.pendingCount > 0;
    },
    get countText() {
      return String(this.pendingCount);
    },
    get badgeText() {
      return `Requests ${this.countText}`;
    },
    get badgeName() {
      return requestsLabel(this.pendingCount);
    },

    // ── lifecycle ────────────────────────────────────────────────────────
    init() {
      try {
        this.projectId = readSeed(this.$el)?.project_id ?? "";
      } catch (err) {
        this.error = describeError(err);
        return;
      }
      if (this.projectId === "") return;
      this.load();
      unfollow = gateway.follow(
        this.projectId,
        (event) => this.arrived(event),
        (status) => {
          if (status === FeedStatus.RESYNCED) this.load();
        },
      );
    },

    destroy() {
      unfollow();
    },

    async load() {
      try {
        const list = await gateway.listRequests(this.projectId, { state: "pending" });
        this.requests = list.map(stripValues).reduce(upsertRequest, []);
        this.error = null;
      } catch (err) {
        this.error = describeError(err);
      } finally {
        this.loaded = true;
      }
    },

    /** One event of the feed: a request filed, answered, completed or expired. */
    arrived(event) {
      if (event.kind !== "service_request" || !event.request) return;
      this.requests = upsertRequest(this.requests, stripValues(event.request));
    },
  };
};
