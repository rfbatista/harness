// A delegate's status-check loop, in its detail header: how often the
// architect is woken to check on it, and the person's controls to pause,
// resume or retune it. The page keys the bar on the loop's interval and state,
// so a change from anywhere (the architect's set_status_check, another tab)
// mounts a fresh one; its words (bar.detail) come from the page, which ticks.
//
//   <template x-for="bar in statusCheckBars" x-bind:key="bar.key">
//     <div x-data="sessionsStatusCheck(bar)"> …
//
// A change is told to the page with status-check-changed, so the row and the
// header follow at once; the session change the server then announces says
// the same.

import { DEFAULT_INTERVAL, INTERVALS } from "../../domain/channel.js";
import { describeError } from "../../../../shared/presentation/errors.js";

/** @param {{ channel: import("../../domain/ports.js").ChannelGateway }} deps */
export const statusCheck = ({ channel }) => (bar = {}) => {
  const check = bar.check ?? null;
  return {
    sessionId: bar.sessionId ?? "",
    /** The interval the select shows, in minutes, as a string (a <select> value). */
    minutes: String(check?.everyMinutes || bar.remembered || DEFAULT_INTERVAL),
    busy: false,
    error: null,

    get intervals() {
      return INTERVALS.map((m) => ({ value: String(m), label: m < 60 ? `${m} min` : `${m / 60} h` }));
    },
    get active() {
      return check?.state === "active";
    },
    get paused() {
      return check?.state === "paused";
    },
    /** An ended loop cannot be changed: the delegate is over. */
    get controllable() {
      return this.active || this.paused;
    },
    get toggleWord() {
      return this.active ? "Pause" : "Resume";
    },

    /** Pause an active loop; resume a paused one at the chosen interval. */
    toggle() {
      return this.set(this.active ? 0 : Number(this.minutes));
    },

    /** The select changed: an active loop takes the new interval now; a paused one keeps it for Resume. */
    retune() {
      if (this.active) return this.set(Number(this.minutes));
    },

    async set(everyMinutes) {
      if (this.busy) return;
      this.busy = true;
      this.error = null;
      try {
        const changed = await channel.setStatusCheck(this.sessionId, everyMinutes);
        this.$dispatch("status-check-changed", { check: changed });
      } catch (err) {
        this.error = describeError(err);
      } finally {
        this.busy = false;
      }
    },

    dismissError() {
      this.error = null;
    },
  };
};
