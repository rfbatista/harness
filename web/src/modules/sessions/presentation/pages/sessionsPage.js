// A task's page: its sessions (a task can run several at once) grouped for
// triage in the inner list, followed live, with the selected one's detail
// beside it. It follows the whole project's feed and keeps its task's sessions.
//
//   <main x-data="sessionsPage" data-seed="sessions-seed"> … </main>

import { FeedStatus } from "../../../../shared/domain/feed.js";
import { describeError } from "../../../../shared/presentation/errors.js";
import { readSeed } from "../../../../shared/presentation/seed.js";
import { Codes, codeOf } from "../../../../shared/domain/errors.js";
import { applyChange, displayOrder, group, INITIAL_TERMINAL_SIZE, isTerminal, ofTask } from "../../domain/session.js";
import { artifactTitle } from "../artifactView.js";
import { startedBy, summary, toDetailView, toGroupViews } from "../view.js";

const TICK_MS = 30_000;

/**
 * @param {{
 *   gateway: import("../../domain/ports.js").SessionGateway,
 *   clock: import("../../../../shared/infrastructure/clock.js").Clock,
 * }} deps
 */
/** How long a session that arrived over the feed stays highlighted. */
export const FRESH_MS = 8_000;

export const sessionsPage = ({ gateway, clock, setTimeout = globalThis.setTimeout.bind(globalThis) }) => () => {
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
    repositoryNames: {},
    error: null,
    stopping: false,
    /** The session a resume call is in flight for. */
    resumingId: null,
    ready: false,
    /** The detail pane shows the new-session form. */
    creating: false,
    /** The selected session's delete is awaiting confirmation. */
    confirmingDelete: false,
    deleting: false,
    /** The detail pane's tab: the agent's terminal, the application run from the worktree, or what the session designed. */
    detailTab: "agent",
    /** Sessions that just arrived over the feed (started elsewhere: by an agent, the TUI). */
    freshIds: [],
    /** Read out by a polite live region when one arrives. */
    announcement: "",
    /** Publishes that arrived while the Design tab was not in front. */
    unseenArtifacts: 0,

    // ── what the markup binds ────────────────────────────────────────────
    get groups() {
      return toGroupViews(this.sessions, {
        selectedId: this.selectedId,
        now: this.now,
        agentNames: this.agentNames,
        fresh: new Set(this.freshIds),
      });
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
    get canResumeSelected() {
      return this.selected?.resumable === true;
    },
    get resumingSelected() {
      return this.resumingId !== null && this.resumingId === this.selectedId;
    },
    get cannotResume() {
      return !this.ready || this.resumingId !== null;
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
      const session = this.sessions.find((s) => s.id === this.selectedId);
      return [
        {
          key: this.selected.id,
          sessionId: this.selected.id,
          repositoryId: this.selected.repositoryId,
          repositoryName: this.repositoryNames[this.selected.repositoryId] ?? "",
          branch: session?.branch ?? "",
          worktree: session?.workingDir ?? "",
          projectId: this.projectId,
        },
      ];
    },
    /** The selected session's git history page; "" for a session without a worktree. */
    get historyHref() {
      const session = this.showingSession ? this.sessions.find((s) => s.id === this.selectedId) : null;
      if (!session?.workspaceId) return "";
      const p = encodeURIComponent(this.projectId);
      const t = encodeURIComponent(this.ticketId);
      return `/projects/${p}/tasks/${t}/sessions/${encodeURIComponent(session.id)}/history`;
    },
    get appUnavailable() {
      return this.showingApp && !this.selected.repositoryId;
    },
    get showingDesign() {
      return this.showingSession && this.detailTab === "design";
    },
    /** The session whose Design panel to mount, keyed on its id; mounted whenever a session shows, so publishes are counted behind the other tabs. */
    get designPanels() {
      if (!this.showingSession) return [];
      // Keyed on liveness too: the panel reads it once, so a resumed session gets a fresh one that follows again.
      const live = this.selected.stoppable;
      return [{ key: `${this.selected.id}:${live ? "live" : "ended"}`, sessionId: this.selected.id, live }];
    },
    get designBadge() {
      return this.unseenArtifacts > 0 ? String(this.unseenArtifacts) : "";
    },
    get designTabSelected() {
      return this.detailTab === "design";
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
        this.repositoryNames = seed.repositoryNames;
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
      const before = change.kind === "upsert" ? this.sessions.find((s) => s.id === change.session.id) : undefined;
      const arriving = change.kind === "upsert" && !before;
      if (before && isTerminal(before) && !isTerminal(change.session)) this.resumed(change.session);
      this.sessions = applyChange(this.sessions, change, ofTask(this.ticketId));
      if (arriving && this.sessions.some((s) => s.id === change.session.id)) this.arrived(change.session);
      this.now = clock.now();
      if (change.kind === "deleted" && change.id === this.selectedId) this.selectedId = null;
      // The session's Design panel follows its own stream; tell it the session is over.
      if (change.kind === "upsert" && isTerminal(change.session)) this.$dispatch("session-ended", { id: change.session.id });
    },

    /** A session started elsewhere joined the task: highlight it for a moment and say so. */
    arrived(session) {
      const by = startedBy(session, this.sessions, this.agentNames);
      const title = session.task || "Untitled session";
      this.announcement = by ? `${by} started a session: ${title}` : `A session started: ${title}`;
      this.freshIds = [...this.freshIds, session.id];
      setTimeout(() => {
        this.freshIds = this.freshIds.filter((id) => id !== session.id);
      }, FRESH_MS);
    },

    /** A session came back: here, in another tab, or in the TUI. */
    resumed(session) {
      this.announcement = `Session resumed: ${session.task || "Untitled session"}`;
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
    showDesign() {
      this.detailTab = "design";
      this.unseenArtifacts = 0;
    },

    /** @param {CustomEvent<{ artifact: import("../../domain/artifact.js").Artifact, isNew: boolean }>} event */
    artifactPublished(event) {
      const { artifact, isNew } = event.detail;
      const title = artifactTitle(artifact);
      this.announcement = isNew ? `New artifact: ${title}` : `Artifact updated: ${title}, revision ${artifact.revision}`;
      if (!this.showingDesign) this.unseenArtifacts += 1;
    },

    /**
     * A move between task and project: announced, not counted; there is
     * nothing new to look at.
     * @param {CustomEvent<{ artifact: import("../../domain/artifact.js").Artifact }>} event
     */
    artifactMoved(event) {
      const { artifact } = event.detail;
      this.announcement = `${artifact.scope === "project" ? "Moved to project" : "Moved back to task"}: ${artifactTitle(artifact)}`;
    },

    get agentTabSelected() {
      return this.detailTab === "agent";
    },
    get appTabSelected() {
      return this.detailTab === "app";
    },

    select(id) {
      if (id !== this.selectedId) this.unseenArtifacts = 0;
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
      const order = displayOrder(this.sessions);
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

    /** Resumes an ended session (the selected one, or a row's) on the server; its terminal attaches once it runs. */
    async resume(id = this.selectedId) {
      if (!id || this.resumingId !== null) return;
      this.resumingId = id;
      this.select(id);
      try {
        const session = await gateway.resume(id, INITIAL_TERMINAL_SIZE);
        this.sessions = applyChange(this.sessions, { kind: "upsert", session }, ofTask(this.ticketId));
        this.now = clock.now();
        this.detailTab = "agent";
        this.error = null;
        this.resumed(session);
      } catch (err) {
        this.error = describeError(err);
        // Someone else resumed it first: the list catches up with them.
        if (codeOf(err) === Codes.SESSION_ALREADY_RUNNING) await this.reload();
      } finally {
        this.resumingId = null;
      }
    },

    dismissError() {
      this.error = null;
    },
  };
};
