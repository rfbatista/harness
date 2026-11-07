// A service's log in a read-only viewer: the last part of the log, followed live
// from where the read ended, a view that stays where the person scrolled to and
// counts the lines that arrive below it, and a separator where a new run began.
// Not a terminal: no input, no PTY.
//
//   <section x-data="serviceLog(id)"> … <div role="log" x-ref="log" x-on:scroll="onScroll">

import { FeedStatus } from "../../../../shared/domain/feed.js";
import { systemClock } from "../../../../shared/infrastructure/clock.js";
import { describeError } from "../../../../shared/presentation/errors.js";
import { applyChunk, applyReset, emptyLog, fromRead, tailNote } from "../../domain/log.js";

/** How close to the bottom still counts as at the bottom, in pixels. */
const BOTTOM_SLACK = 8;

/**
 * @param {{
 *   gateway: import("../../domain/ports.js").ServicesGateway,
 *   clock?: import("../../../../shared/infrastructure/clock.js").Clock,
 * }} deps
 */
export const serviceLog = ({ gateway, clock = systemClock }) => (id = "") => {
  let unfollow = () => {};
  /** The log's scrolling element, kept from init (a method's $refs resolve from its caller). */
  let viewport = null;
  let serviceId = id;

  return {
    /** @type {import("../../domain/log.js").LogLine[]} */
    lines: [],
    /** The byte offset the log is current to. */
    end: 0,
    runId: "",
    earlier: false,
    loading: true,
    live: false,
    error: null,
    /** Whether the view follows new lines: true while it is at the bottom. */
    following: true,
    /** Complete lines that arrived while the view was scrolled up. */
    unseen: 0,

    // ── what the markup binds ────────────────────────────────────────────
    get note() {
      return tailNote({ earlier: this.earlier, lines: this.lines, end: this.end, runId: this.runId });
    },
    get hasNote() {
      return this.note !== "";
    },
    get empty() {
      return !this.loading && this.error === null && this.lines.length === 0;
    },
    get footer() {
      return this.following ? "Following" : "Paused · scrolled up";
    },
    get unseenText() {
      return this.unseen === 0 ? "" : `${this.unseen} new line${this.unseen === 1 ? "" : "s"} below`;
    },
    get hasUnseen() {
      return this.unseen > 0;
    },

    // ── lifecycle ────────────────────────────────────────────────────────
    async init() {
      serviceId = id || (this.$el.dataset.serviceId ?? "");
      viewport = this.$refs.log ?? null;
      await this.load();
    },

    destroy() {
      unfollow();
    },

    /** Reads the last part of the log, then follows the feed from where that read ended. */
    async load() {
      unfollow();
      this.loading = true;
      this.error = null;
      try {
        this.show(fromRead(await gateway.readLog(serviceId)));
        this.follow();
      } catch (err) {
        this.error = describeError(err);
      } finally {
        this.loading = false;
      }
    },

    retry() {
      return this.load();
    },

    follow() {
      unfollow = gateway.followLog(
        serviceId,
        this.end,
        (event) => this.arrived(event),
        (status) => this.feedStatus(status),
      );
    },

    show(log) {
      this.lines = log.lines;
      this.end = log.end;
      this.runId = log.runId;
      this.earlier = log.earlier;
      this.following = true;
      this.unseen = 0;
      this.$nextTick(() => this.toBottom());
    },

    held() {
      return { lines: this.lines, end: this.end, runId: this.runId, earlier: this.earlier };
    },

    feedStatus(status) {
      this.live = status === FeedStatus.LIVE;
      if (status === FeedStatus.RESYNCED) this.reread();
    },

    /** One event of the log feed. */
    arrived(event) {
      if (event.kind === "reset") {
        this.show(applyReset(this.held(), event, this.clockTime()));
        return;
      }
      const next = applyChunk(this.held(), event);
      if (next.gap) {
        this.reread();
        return;
      }
      this.take(next.log, next.added);
    },

    take(log, added) {
      this.lines = log.lines;
      this.end = log.end;
      this.earlier = log.earlier;
      if (this.following) this.$nextTick(() => this.toBottom());
      else this.unseen += added;
    },

    /** The log from the end of what is held: a gap, or a feed that was missed events. */
    async reread() {
      try {
        const answer = await gateway.readLog(serviceId, this.end);
        if (answer.run_id !== this.runId && this.runId !== "") {
          this.show(fromRead({ ...answer, offset: 0 }));
          return;
        }
        const next = applyChunk(this.held(), { offset: this.end, text: answer.text, run_id: answer.run_id });
        this.take(next.log, next.added);
      } catch (err) {
        this.error = describeError(err);
      }
    },

    clockTime() {
      const d = clock.now();
      return `${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}`;
    },

    // ── scrolling ────────────────────────────────────────────────────────
    /** @param {{ currentTarget: HTMLElement }} event the log's scroll */
    onScroll(event) {
      const el = event.currentTarget;
      const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight <= BOTTOM_SLACK;
      this.following = atBottom;
      if (atBottom) this.unseen = 0;
    },

    toBottom() {
      if (viewport) viewport.scrollTop = viewport.scrollHeight;
    },

    /** Jump to latest. */
    jump() {
      this.following = true;
      this.unseen = 0;
      this.$nextTick(() => this.toBottom());
    },

    dismissError() {
      this.error = null;
    },
  };
};
