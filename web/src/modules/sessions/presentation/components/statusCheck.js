// A delegate's status-check loop: how often the architect is woken to check
// on it, and the person's controls to pause, resume or retune it. In the
// session head it is a button ("Checks · 20m · next 15m") opening a panel in
// the top layer; on a phone it is the first row of the ⋯ sheet. The page keys
// it on the loop's interval and state, so a change from anywhere (the
// architect's set_status_check, another tab) mounts a fresh one; its words
// about time (bar.next, bar.last) come from the page, which ticks. The
// button's words and the select read the same value, so they never disagree.
//
//   <template x-for="bar in statusCheckBars" x-bind:key="bar.key">
//     <div style="display: contents" x-data="sessionsStatusCheck(bar)">
//       <button x-ref="button" x-on:click="openPanel" …>…</button>
//       <div class="[ popup ]" role="dialog" id="session-checks" x-data="popupDialog" …>…</div>
//     </div>
//   </template>
//
// A change is told to the page with status-check-changed, so the row and the
// head follow at once; the session change the server then announces says the
// same.

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
    /** The interval last asked for, for Retry. */
    asked: null,

    /** The presets, plus the loop's own interval when the architect set another (else the select would show the first preset). */
    get intervals() {
      const own = check?.everyMinutes || bar.remembered || 0;
      const all = INTERVALS.includes(own) || !(own > 0) ? INTERVALS : [...INTERVALS, own].sort((a, b) => a - b);
      return all.map((m) => ({ value: String(m), label: m < 60 || m % 60 ? `${m} min` : `${m / 60} h` }));
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
      return this.active ? "Pause checks" : "Resume checks";
    },
    /** The interval the select shows, short: 20m, 1h. */
    get every() {
      const m = Number(this.minutes);
      return m < 60 || m % 60 ? `${m}m` : `${m / 60}h`;
    },
    get buttonDot() {
      if (this.error) return "failed";
      return this.paused ? "idle" : "live";
    },
    get buttonNext() {
      if (this.busy) return "saving…";
      if (this.error) return "couldn't save";
      if (this.paused) return "paused";
      return (bar.next ?? "").replace(/^next in /, "next ");
    },
    get buttonLabel() {
      const next = this.busy || this.error || this.paused ? this.buttonNext : bar.next ?? "";
      return `Status checks: every ${this.every}, ${next}`;
    },
    get panelDot() {
      return this.paused ? "idle" : "live";
    },
    get panelWord() {
      return this.paused ? "Paused" : "On";
    },
    get nextWords() {
      return this.paused ? "" : bar.next ?? "";
    },
    get lastWords() {
      return bar.last ?? "";
    },

    /** Opens the panel under the button. */
    openPanel() {
      this.$dispatch("dialog-open", { id: "session-checks", anchor: this.$refs.button });
    },

    /** Sends the change that failed again. */
    retry() {
      if (this.asked !== null) return this.set(this.asked);
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
      this.asked = everyMinutes;
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
