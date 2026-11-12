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
import { Codes, codeOf } from "../../../../shared/domain/errors.js";
import { isPending, pendingCount, requestCard, requestIdFromHash, requestsLabel, stripValues, upsertRequest } from "../../domain/request.js";

const POPOVER_ID = "services-requests";
const BUTTON_ID = "services-requests-button";

/**
 * @param {{
 *   gateway: import("../../domain/ports.js").ServicesGateway,
 *   clock?: import("../../../../shared/infrastructure/clock.js").Clock,
 *   hash?: () => string,  the page's URL fragment: an inbox link names a request (#service-request-<id>)
 * }} deps
 */
export const serviceRequests = ({ gateway, clock = systemClock, hash = () => globalThis.location?.hash ?? "" }) => () => {
  let unfollow = () => {};
  /** The component's root, kept from init: a card's button is gone by the time a focus is asked for. */
  let root = null;
  let linkFollowed = false;

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
    /** The note written per request, and whether its answer is in flight. */
    drafts: {},
    /** Requests that stay as an outcome card until the popover closes: answered here or elsewhere while it was open. */
    outcomes: {},
    /** The request whose answer is in flight: its echo on the feed is not someone else's. */
    answeringId: "",
    announcement: "",
    /** The request whose card has the focus when the popover opens ("" for the list's heading). */
    focusedId: "",
    /** Said above the cards when an inbox link names a request that no longer waits. */
    linkNote: "",

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
      return this.requests
        .filter((r) => isPending(r) || this.outcomes[r.id] === true)
        .map((r) => ({
          ...requestCard(this.withValues(r), { now: this.now, projectId: this.projectId, revealed: this.revealed[r.id] ?? {} }),
          draft: this.draftOf(r.id),
          noteId: `service-request-note-${r.id}`,
        }));
    },
    /** The request as last heard of, with the variable values the popover read (none while it is closed). */
    withValues(request) {
      const read = this.details[request.id];
      return read ? { ...request, env: read.env, proposed: read.proposed ?? request.proposed } : request;
    },
    draftOf(id) {
      return this.drafts[id] ?? { note: "", busy: false };
    },
    ensureDraft(id) {
      if (!this.drafts[id]) this.drafts = { ...this.drafts, [id]: { note: "", busy: false } };
    },
    get headline() {
      const n = this.pendingCount;
      return `${n} request${n === 1 ? " waits" : "s wait"} on you`;
    },

    // ── lifecycle ────────────────────────────────────────────────────────
    init() {
      root = this.$el;
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
        for (const r of list) this.ensureDraft(r.id);
        this.requests = list.map(stripValues).reduce(upsertRequest, []);
        this.error = null;
      } catch (err) {
        this.error = describeError(err);
      } finally {
        this.loaded = true;
      }
      this.followLink(hash());
    },

    /** The page was opened on an inbox link (#service-request-<id>): open the popover at that request, once. */
    followLink(target) {
      if (linkFollowed) return;
      linkFollowed = true;
      this.showLink(target);
    },

    /** The fragment changed while the page was open: an inbox link was followed again. */
    hashChanged(target) {
      this.showLink(typeof target === "string" ? target : hash());
    },

    showLink(target) {
      const id = requestIdFromHash(target);
      if (id === "") return;
      const waiting = this.requests.some((r) => r.id === id && isPending(r));
      this.linkNote = waiting ? "" : "That request is no longer waiting on you.";
      return this.showRequest(waiting ? id : "");
    },

    /** A row's badge asked for its service's requests: open at the first of them. */
    showFor(event) {
      const serviceId = event?.detail?.serviceId ?? "";
      const first = this.requests.find((r) => r.serviceId === serviceId && isPending(r));
      return this.showRequest(first?.id ?? "");
    },

    /** Opens the popover and focuses the request's card (the list's heading when id is ""). */
    async showRequest(id) {
      this.focusedId = id;
      await this.openPopover();
      this.$nextTick(() => {
        const card = [...(root?.querySelectorAll?.("article[data-service-request]") ?? [])].find((a) => a.dataset.requestId === id);
        const heading = card?.querySelector("[data-service-request-heading]") ?? root?.querySelector?.("[data-service-requests-heading]");
        heading?.scrollIntoView?.({ block: "nearest" });
        heading?.focus?.();
      });
    },

    /** One event of the feed: a request filed, answered, completed or expired. */
    arrived(event) {
      if (event.kind !== "service_request" || !event.request) return;
      const before = this.requests.find((r) => r.id === event.request.id);
      this.ensureDraft(event.request.id);
      this.requests = upsertRequest(this.requests, stripValues(event.request));
      if (event.request.id === this.answeringId) return; // the person's own answer
      if (this.popoverOpen && isPending(event.request)) this.readDetails();
      else if (this.popoverOpen && before && isPending(before)) {
        this.outcomes = { ...this.outcomes, [event.request.id]: true };
        this.say("A service request was answered elsewhere");
      }
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
      this.outcomes = {};
      this.linkNote = "";
      this.focusedId = "";
      this.requests = this.requests.filter(isPending);
    },

    /** The list call, for the values of the pending requests. Never run in the background. */
    async readDetails() {
      try {
        const list = await gateway.listRequests(this.projectId, { state: "pending" });
        if (!this.popoverOpen) return;
        this.details = Object.fromEntries(list.map((r) => [r.id, r]));
        for (const r of list) this.ensureDraft(r.id);
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

    // ── the person's answer ──────────────────────────────────────────────
    approve(id) {
      return this.answer(id, "approve");
    },
    deny(id) {
      return this.answer(id, "deny");
    },

    async answer(id, decision) {
      const draft = this.drafts[id];
      if (!draft || draft.busy) return;
      draft.busy = true;
      this.answeringId = id;
      this.error = null;
      try {
        const answered = stripValues(await gateway.answerRequest(id, decision, draft.note.trim()));
        this.requests = upsertRequest(this.requests, answered);
        this.outcomes = { ...this.outcomes, [id]: true };
        this.say(decision === "approve" ? "You approved the service request" : "You denied the service request");
      } catch (err) {
        const refusal = describeError(err);
        if (codeOf(err) === Codes.REQUEST_NOT_PENDING) await this.readDetails();
        this.error = refusal;
      } finally {
        draft.busy = false;
        this.answeringId = "";
      }
    },

    say(text) {
      this.announcement = text;
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
