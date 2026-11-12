// The service requests agents wait on the person for (ADR 0083): the "Requests N"
// badge in the Services page toolbar, counted from the project's pending requests
// and kept live by the services feed (contract: Harness server ↔ Web UI — services,
// section 13). A request is kept here without its variable values: they are read
// with the list call when the person opens the requests, never from the feed.
//
//   <span x-data="serviceRequests" data-seed="services-seed"> … </span>

import { systemClock } from "../../../../shared/infrastructure/clock.js";
import { FeedStatus } from "../../../../shared/domain/feed.js";
import { describeError } from "../../../../shared/presentation/errors.js";
import { readSeed } from "../../../../shared/presentation/seed.js";
import { isPending, pendingCount, requestCard, requestsLabel, stripValues, upsertRequest } from "../../domain/request.js";

const POPOVER_ID = "services-requests";
const BUTTON_ID = "services-requests-button";

/** @param {{ gateway: import("../../domain/ports.js").ServicesGateway, clock?: import("../../../../shared/infrastructure/clock.js").Clock }} deps */
export const serviceRequests = ({ gateway, clock = systemClock }) => () => {
  let unfollow = () => {};

  return {
    projectId: "",
    /** @type {import("../../domain/ports.js").ServiceRequest[]} without values */
    requests: [],
    loaded: false,
    error: null,
    popoverOpen: false,
    /** The pending requests as the list call returned them, with their values: only while the popover is open. */
    details: {},
    /** The variables shown in clear, per request: { id: { NAME: true } }. In memory only. */
    revealed: {},
    now: clock.now(),

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
    get badgeName() {
      return requestsLabel(this.pendingCount);
    },

    /** The cards of the pending requests, newest first; with their values once the popover has read them. */
    get cards() {
      return this.requests.filter(isPending).map((r) => requestCard(this.details[r.id] ?? r, { now: this.now, projectId: this.projectId, revealed: this.revealed[r.id] ?? {} }));
    },
    get headline() {
      const n = this.pendingCount;
      return `${n} request${n === 1 ? " waits" : "s wait"} on you`;
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
      if (this.popoverOpen && isPending(event.request)) this.readDetails();
    },

    // ── the popover ──────────────────────────────────────────────────────
    /** Opens the popover and reads the pending requests for their values; values are kept only while it is open. */
    async openPopover() {
      this.now = clock.now();
      this.popoverOpen = true;
      this.$dispatch("dialog-open", { id: POPOVER_ID, anchor: globalThis.document?.getElementById?.(BUTTON_ID) ?? null });
      await this.readDetails();
    },

    closePopover() {
      this.$dispatch("dialog-dismiss", { id: POPOVER_ID });
    },

    /** The popover closed, however it did: the values and what was revealed are dropped. */
    popoverClosed(event) {
      if (event?.detail?.id !== POPOVER_ID) return;
      this.popoverOpen = false;
      this.details = {};
      this.revealed = {};
    },

    /** The list call, for the values of the pending requests. Never run in the background. */
    async readDetails() {
      try {
        const list = await gateway.listRequests(this.projectId, { state: "pending" });
        if (!this.popoverOpen) return;
        this.details = Object.fromEntries(list.map((r) => [r.id, r]));
        this.requests = list.map(stripValues).reduce(upsertRequest, this.requests.filter((r) => !isPending(r)));
        this.error = null;
      } catch (err) {
        this.error = describeError(err);
      }
    },

    retry() {
      return this.readDetails();
    },

    dismissError() {
      this.error = null;
    },

    /** @param {{ currentTarget: HTMLElement }} event a variable's Reveal or Hide: the request and variable are on it */
    toggle(event) {
      const { requestId, name } = event.currentTarget.dataset;
      const own = this.revealed[requestId] ?? {};
      this.revealed = { ...this.revealed, [requestId]: { ...own, [name]: own[name] !== true } };
    },

    /** @param {{ currentTarget: HTMLElement }} event a card's Reveal all or Hide all */
    toggleAll(event) {
      const id = event.currentTarget.dataset.requestId;
      const request = this.details[id];
      if (!request) return;
      const names = request.env.filter((e) => e.change === "set").map((e) => e.name);
      const own = this.revealed[id] ?? {};
      const reveal = !names.every((n) => own[n] === true);
      this.revealed = { ...this.revealed, [id]: reveal ? Object.fromEntries(names.map((n) => [n, true])) : {} };
    },
  };
};
