// The Sessions page: a project's sessions, grouped for triage, followed live,
// with the selected one's detail beside the list.
//
//   <main x-data="sessionsPage" data-seed="sessions-seed"> … </main>

import { FeedStatus } from "../../../../shared/domain/feed.js";
import { describeError } from "../../../../shared/presentation/errors.js";
import { readSeed } from "../../../../shared/presentation/seed.js";
import { applyChange, byRecent, group } from "../../domain/session.js";
import { summary, toDetailView, toGroupViews } from "../view.js";

const TICK_MS = 30_000;

/**
 * @param {{
 *   gateway: import("../../domain/ports.js").SessionGateway,
 *   clock: import("../../../../shared/infrastructure/clock.js").Clock,
 * }} deps
 */
export const sessionsPage = ({ gateway, clock }) => () => {
  let unfollow = () => {};
  let ticker = null;

  return {
    projectId: "",
    /** @type {import("../../domain/session.js").Session[]} */
    sessions: [],
    selectedId: null,
    now: clock.now(),
    error: null,
    stopping: false,
    ready: false,

    // ── what the markup binds ────────────────────────────────────────────
    get groups() {
      return toGroupViews(this.sessions, { selectedId: this.selectedId, now: this.now });
    },
    get selected() {
      const session = this.sessions.find((s) => s.id === this.selectedId);
      return session ? toDetailView(session, this.now) : null;
    },
    get hasSelection() {
      return this.selected !== null;
    },
    get isEmpty() {
      return this.sessions.length === 0;
    },
    get summary() {
      return summary(this.sessions);
    },
    get cannotStop() {
      return !this.ready || this.stopping || !this.selected?.stoppable;
    },

    // ── lifecycle ────────────────────────────────────────────────────────
    init() {
      try {
        const seed = gateway.decodeSeed(readSeed(this.$el));
        this.projectId = seed.projectId;
        this.sessions = seed.sessions;
      } catch (err) {
        this.error = describeError(err);
        return;
      }
      this.selectedId = group(this.sessions)[0]?.sessions[0]?.id ?? null;

      unfollow = gateway.follow(
        this.projectId,
        (change) => this.apply(change),
        (status) => this.feedStatus(status),
      );
      ticker = setInterval(() => (this.now = clock.now()), TICK_MS);

      // Alpine has rendered the live list; drop the server-rendered copy.
      this.$nextTick(() => {
        for (const node of this.$el.querySelectorAll("[data-ssr]")) node.remove();
        this.ready = true;
      });
    },

    destroy() {
      unfollow();
      clearInterval(ticker);
    },

    // ── feed ─────────────────────────────────────────────────────────────
    apply(change) {
      this.sessions = applyChange(this.sessions, change);
      this.now = clock.now();
      if (change.kind === "deleted" && change.id === this.selectedId) this.selectedId = null;
    },

    feedStatus(status) {
      this.$dispatch("feed-status", status);
      if (status === FeedStatus.RESYNCED) this.reload();
    },

    async reload() {
      try {
        this.sessions = await gateway.list(this.projectId);
        this.now = clock.now();
      } catch (err) {
        this.error = describeError(err);
      }
    },

    // ── developer actions ────────────────────────────────────────────────
    select(id) {
      this.selectedId = id;
    },

    next() {
      this.step(1);
    },
    previous() {
      this.step(-1);
    },
    step(delta) {
      const order = group(byRecent(this.sessions)).flatMap((g) => g.sessions.map((s) => s.id));
      if (order.length === 0) return;
      const at = order.indexOf(this.selectedId);
      const next = at === -1 ? 0 : Math.max(0, Math.min(order.length - 1, at + delta));
      this.selectedId = order[next];
    },

    async stopSelected() {
      if (this.cannotStop) return;
      this.stopping = true;
      try {
        await gateway.stop(this.selectedId);
      } catch (err) {
        this.error = describeError(err);
      } finally {
        this.stopping = false;
      }
    },

    replySent() {
      this.error = null;
    },

    dismissError() {
      this.error = null;
    },
  };
};
