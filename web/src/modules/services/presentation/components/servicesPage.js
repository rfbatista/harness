// The Services page: every service of the project with its live status. The
// server seeds the list (the answer of list_services); the page then follows
// the project's services feed. An event at or below the list's seq is already
// in it; a gap, or a resynced feed, reads the list again (contract: Harness
// server ↔ Web UI — services, section 6).
//
//   <main x-data="servicesPage" data-seed="services-seed"> …

import { FeedStatus } from "../../../../shared/domain/feed.js";
import { systemClock } from "../../../../shared/infrastructure/clock.js";
import { describeError } from "../../../../shared/presentation/errors.js";
import { readSeed } from "../../../../shared/presentation/seed.js";
import { applyEvent, groupServices, runningCount } from "../../domain/service.js";
import { serviceRow } from "../view.js";

/** How often the times on the rows move on. */
const TICK_MS = 15_000;

/**
 * @param {{
 *   gateway: import("../../domain/ports.js").ServicesGateway,
 *   clock?: import("../../../../shared/infrastructure/clock.js").Clock,
 *   timers?: { setInterval: typeof setInterval, clearInterval: typeof clearInterval },
 * }} deps
 */
export const servicesPage = ({ gateway, clock = systemClock, timers = globalThis }) => () => {
  let unfollow = () => {};
  let ticker = null;

  return {
    projectId: "",
    /** @type {import("../../domain/ports.js").Service[]} */
    services: [],
    /** The services feed's sequence number the list is current to. */
    seq: 0,
    /** @type {{ name: "docker" | "podman", available: boolean }} */
    runtime: { name: "docker", available: true },
    /** @type {"on" | "off" | "unavailable"} */
    sandbox: "on",
    /** The time the rows are drawn at. */
    now: clock.now(),
    loading: false,
    live: false,
    error: null,

    // ── what the markup binds ────────────────────────────────────────────
    get groups() {
      return groupServices(this.services).map((g) => ({
        key: g.key,
        label: g.label,
        tone: g.key === "running" ? "signal" : "",
        count: g.services.length,
        rows: g.services.map((s) => serviceRow(s, this.now)),
      }));
    },
    get empty() {
      return !this.loading && this.error === null && this.services.length === 0;
    },
    get skeleton() {
      return this.loading && this.services.length === 0;
    },
    get runningCount() {
      return runningCount(this.services);
    },
    get hasRunning() {
      return this.runningCount > 0;
    },
    get runningLabel() {
      return `${this.runningCount} running`;
    },

    // ── lifecycle ────────────────────────────────────────────────────────
    init() {
      try {
        const seed = gateway.decodeSeed(readSeed(this.$el));
        this.projectId = seed.projectId;
        if (seed.list) this.show(seed.list);
      } catch (err) {
        this.error = describeError(err);
        return;
      }
      if (this.seq === 0 && this.services.length === 0) this.load();
      unfollow = gateway.follow(
        this.projectId,
        (event) => this.arrived(event),
        (status) => this.feedStatus(status),
      );
      ticker = timers.setInterval(() => { this.now = clock.now(); }, TICK_MS);
    },

    destroy() {
      unfollow();
      if (ticker !== null) timers.clearInterval(ticker);
    },

    feedStatus(status) {
      this.live = status === FeedStatus.LIVE;
      if (status === FeedStatus.RESYNCED) this.load();
    },

    /** One event of the feed. */
    arrived(event) {
      const next = applyEvent({ seq: this.seq, services: this.services }, event);
      if (next.reload) {
        this.load();
        return;
      }
      if (!next.applied) return;
      this.services = next.state.services;
      this.seq = next.state.seq;
    },

    show(list) {
      this.services = list.services;
      this.seq = list.seq;
      this.runtime = list.runtime;
      this.sandbox = list.sandbox;
    },

    /** Reads the list. Events the feed delivers meanwhile are at or below its seq, or a gap again. */
    async load() {
      this.loading = true;
      this.error = null;
      try {
        this.show(await gateway.listServices(this.projectId));
      } catch (err) {
        this.error = describeError(err);
      } finally {
        this.loading = false;
      }
    },

    retry() {
      return this.load();
    },

    dismissError() {
      this.error = null;
    },
  };
};
