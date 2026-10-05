// A task's page: its sessions (a task can run several at once) grouped for
// triage in the inner list, followed live, with the selected one's detail
// beside it. It follows the whole project's feed and keeps its task's sessions.
//
//   <main x-data="sessionsPage" data-seed="sessions-seed"> … </main>

import { FeedStatus } from "../../../../shared/domain/feed.js";
import { describeError } from "../../../../shared/presentation/errors.js";
import { readSeed } from "../../../../shared/presentation/seed.js";
import { applyChange, byRecent, group, ofTask } from "../../domain/session.js";
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
    ticketId: "",
    /** @type {import("../../domain/session.js").Session[]} */
    sessions: [],
    selectedId: null,
    now: clock.now(),
    agentNames: {},
    error: null,
    stopping: false,
    ready: false,
    /** The detail pane shows the new-session form. */
    creating: false,
    /** The selected session's delete is awaiting confirmation. */
    confirmingDelete: false,
    deleting: false,
    /** The detail pane's tab: the agent's terminal, or the application run from the worktree. */
    detailTab: "agent",

    // ── what the markup binds ────────────────────────────────────────────
    get groups() {
      return toGroupViews(this.sessions, { selectedId: this.selectedId, now: this.now, agentNames: this.agentNames });
    },
    get selected() {
      const session = this.sessions.find((s) => s.id === this.selectedId);
      return session ? toDetailView(session, this.now, this.agentNames) : null;
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
    get cannotCreate() {
      return !this.ready || this.creating;
    },
    get showingSession() {
      return !this.creating && this.hasSelection;
    },
    get showingAgent() {
      return this.showingSession && this.detailTab === "agent";
    },
    get showingApp() {
      return this.showingSession && this.detailTab === "app";
    },
    /** The session whose terminal to mount, as a one-item list keyed on its id. */
    get terminalIds() {
      return this.showingAgent && this.selected.terminal === "attach" ? [this.selected.id] : [];
    },
    get terminalNote() {
      return this.showingAgent ? this.selected.terminalNote : "";
    },
    /** The session whose App panel to mount, keyed on its id, with what the panel needs. */
    get appPanels() {
      if (!this.showingApp || !this.selected.repositoryId) return [];
      return [{ key: this.selected.id, sessionId: this.selected.id, repositoryId: this.selected.repositoryId, projectId: this.projectId }];
    },
    get appUnavailable() {
      return this.showingApp && !this.selected.repositoryId;
    },
    get showingNothing() {
      return !this.creating && !this.hasSelection;
    },
    get deleteConsequence() {
      return this.selected?.stoppable
        ? "It is still running: it stops now. Its worktree is removed; its branch and commits stay."
        : "Its record and worktree are removed; its branch and commits stay.";
    },

    // ── lifecycle ────────────────────────────────────────────────────────
    init() {
      try {
        const seed = gateway.decodeSeed(readSeed(this.$el));
        this.projectId = seed.projectId;
        this.ticketId = seed.ticketId;
        this.agentNames = seed.agentNames;
        this.sessions = seed.sessions.filter(ofTask(this.ticketId));
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
      this.sessions = applyChange(this.sessions, change, ofTask(this.ticketId));
      this.now = clock.now();
      if (change.kind === "deleted" && change.id === this.selectedId) this.selectedId = null;
    },

    feedStatus(status) {
      this.$dispatch("feed-status", status);
      if (status === FeedStatus.RESYNCED) this.reload();
    },

    async reload() {
      try {
        this.sessions = await gateway.list({ projectId: this.projectId, ticketId: this.ticketId });
        this.now = clock.now();
      } catch (err) {
        this.error = describeError(err);
      }
    },

    // ── developer actions ────────────────────────────────────────────────
    showAgent() {
      this.detailTab = "agent";
    },
    showApp() {
      this.detailTab = "app";
    },
    get agentTabSelected() {
      return this.detailTab === "agent";
    },
    get appTabSelected() {
      return this.detailTab === "app";
    },

    select(id) {
      this.selectedId = id;
      this.creating = false;
      this.confirmingDelete = false;
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

    // New session: the form is its own component (sessionsNewSession); it
    // reports back with session-created or new-session-cancelled.
    startCreating() {
      if (this.cannotCreate) return;
      this.confirmingDelete = false;
      this.creating = true;
    },

    /** N opens the form, unless the developer is typing in a field. */
    startCreatingFromKey(event) {
      if (event.target?.closest?.("input, textarea, select, [contenteditable]")) return;
      event.preventDefault?.();
      this.startCreating();
    },

    cancelCreating() {
      this.creating = false;
    },

    /** @param {CustomEvent<{ session: import("../../domain/session.js").Session }>} event */
    sessionCreated(event) {
      const { session } = event.detail;
      // The feed reports it too once it runs; adding it now shows it at once.
      this.sessions = applyChange(this.sessions, { kind: "upsert", session }, ofTask(this.ticketId));
      this.selectedId = session.id;
      this.creating = false;
      this.error = null;
    },

    // Delete: ask first, inline, then remove.
    askDelete() {
      if (this.hasSelection) this.confirmingDelete = true;
    },

    cancelDelete() {
      this.confirmingDelete = false;
    },

    async deleteSelected() {
      const id = this.selectedId;
      if (!id || this.deleting) return;
      this.deleting = true;
      try {
        await gateway.remove(id);
        this.sessions = applyChange(this.sessions, { kind: "deleted", id });
        if (this.selectedId === id) this.selectedId = null;
        this.confirmingDelete = false;
      } catch (err) {
        this.error = describeError(err);
      } finally {
        this.deleting = false;
      }
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

    dismissError() {
      this.error = null;
    },
  };
};
