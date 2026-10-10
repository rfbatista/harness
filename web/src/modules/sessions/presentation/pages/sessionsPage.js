// A task's page: its sessions (a task can run several at once) grouped for
// triage in the inner list, followed live, with the selected one's detail
// beside it. It follows the whole project's feed and keeps its task's sessions.
//
//   <main x-data="sessionsPage" data-seed="sessions-seed"> … </main>

import { FeedStatus } from "../../../../shared/domain/feed.js";
import { describeError } from "../../../../shared/presentation/errors.js";
import { readSeed } from "../../../../shared/presentation/seed.js";
import { Codes, codeOf } from "../../../../shared/domain/errors.js";
import { mergeMessages, newestAt, upsertMessage } from "../../domain/channel.js";
import { applyChange, displayOrder, group, INITIAL_TERMINAL_SIZE, isTerminal, ofTask } from "../../domain/session.js";
import { artifactTitle, taskDesignHref } from "../artifactView.js";
import { announceMessage } from "../channelView.js";
import { delegateOption, startedBy, statusCheckView, summary, toDetailView, toGroupViews } from "../view.js";
import { DEFAULT_TEXT_SIZE, nextTextSize, TEXT_SIZES } from "../screenPrefs.js";

const TICK_MS = 30_000;

/** How long a session that arrived over the feed stays highlighted. */
export const FRESH_MS = 8_000;

/**
 * The task's messages between its architect and the delegates, shared by the
 * page and its Conversation tab: Alpine.store("sessionsChannel").
 * @typedef {{ messages: import("../../domain/channel.js").TaskMessage[], loaded: boolean }} ChannelStore
 */

/**
 * @param {{
 *   gateway: import("../../domain/ports.js").SessionGateway,
 *   clock: import("../../../../shared/infrastructure/clock.js").Clock,
 *   channel?: import("../../domain/ports.js").ChannelGateway,  the architect channel; without it the page shows none
 *   channelStore?: ChannelStore,
 *   location?: { search: string },  ?session=<id> opens that session (a comment links to the session that wrote it)
 *   prefs?: ReturnType<typeof import("../screenPrefs.js").screenPrefs>,  this browser's text size and focus mode
 *   shell?: { setFocus: (on: boolean) => void },  the app shell around the page (focus mode hides the rail and more)
 *   clipboard?: { writeText: (text: string) => Promise<void> },
 *   isPhone?: () => boolean,
 * }} deps
 */
export const sessionsPage = ({
  gateway,
  clock,
  channel = null,
  channelStore = { messages: [], loaded: false },
  setTimeout = globalThis.setTimeout.bind(globalThis),
  location = globalThis.location,
  prefs = { textSize: () => DEFAULT_TEXT_SIZE, setTextSize() {}, focus: () => false, setFocus() {} },
  shell = { setFocus() {} },
  clipboard = { writeText: (text) => globalThis.navigator.clipboard.writeText(text) },
  isPhone = () => globalThis.matchMedia?.("(width < 48rem)").matches ?? false,
}) => () => {
  let unfollow = () => {};
  let unfollowChannel = null;
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
    /** On a narrow screen, the detail is shown in place of the list. */
    detailOpen: false,
    deleting: false,
    /** What each mounted agent terminal last said about itself (terminal-status), by session id. */
    terminalStatuses: {},
    /** The same for the App tab's run terminal on screen, and which run that is. */
    runStatus: null,
    shownRunId: "",
    /** A terminal that ended while it was open: its screen stays, read-only, under the note. */
    keptTerminalId: null,
    /** Bumped on a resume, so the resumed session mounts a fresh terminal. */
    terminalEpochs: {},
    /** This browser's terminal text size, in px, and focus mode. */
    textSize: prefs.textSize(),
    focusMode: prefs.focus(),
    /** The detail pane's tab: the agent's terminal, the application run from the worktree, or what the session designed. */
    detailTab: "agent",
    /** Sessions that just arrived over the feed (started elsewhere: by an agent, the TUI). */
    freshIds: [],
    /** Read out by a polite live region when one arrives. */
    announcement: "",
    /** Publishes that arrived while the Design tab was not in front. */
    unseenArtifacts: 0,
    /** Messages to or from the selected session that arrived while its Conversation tab was not in front. */
    unseenMessages: 0,
    /** Each delegate's last non-zero status-check interval, for Resume after a pause. */
    lastIntervals: {},
    /** The task's documents when the page loaded, to name the ones a message links. */
    documentTitles: {},

    // ── what the markup binds ────────────────────────────────────────────
    get groups() {
      return toGroupViews(this.sessions, {
        messages: channelStore.loaded ? channelStore.messages : undefined,
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
    /** The selected session's worktree, as a list keyed by it, so what hangs off it (its PR badge) follows the selection. */
    get selectedWorkspaces() {
      const id = this.selected?.workspaceId;
      return id ? [id] : [];
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
    /** The session whose terminal to mount, as a one-item list keyed on it (and on its resumes), with its id. */
    get terminalPanes() {
      if (!this.showingAgent) return [];
      const id = this.selected.id;
      if (this.selected.terminal !== "attach" && this.keptTerminalId !== id) return [];
      return [{ key: `${id}:${this.terminalEpochs[id] ?? 0}`, id }];
    },
    /** The status of the terminal on screen: the agent's on Agent, the run's on App; null without one. */
    get paneTerminal() {
      if (this.terminalPanes.length > 0) {
        const id = this.terminalPanes[0].id;
        return this.terminalStatuses[id] ?? { id, kind: "agent", state: "connecting", title: "", exitCode: null, lostAt: null, scrolledUp: false, newLines: 0 };
      }
      if (this.showingApp && this.shownRunId && this.runStatus?.id === this.shownRunId) return this.runStatus;
      return null;
    },
    /** The tab row's terminal tools, for the terminal on screen; null on a tab without one. */
    get paneTools() {
      const t = this.paneTerminal;
      if (!t) return null;
      const down = t.state === "connecting" || t.state === "reconnecting" || t.state === "offline";
      const run = t.kind === "run";
      let dotLabel;
      if (t.state === "ended") dotLabel = run ? `Exited with code ${t.exitCode ?? 0}` : `Ended · exit ${t.exitCode ?? 0}`;
      else if (run) dotLabel = "Live · keys go to the app";
      else dotLabel = t.title ? `Live · ${t.title}` : "Live";
      return {
        id: t.id,
        kind: t.kind,
        down,
        statusState: { connecting: "queued", reconnecting: "reconnecting", offline: "offline" }[t.state] ?? "",
        word: { connecting: "connecting…", reconnecting: "reconnecting…", offline: `offline · lost ${lostWords(t.lostAt, this.now)}` }[t.state] ?? "",
        reconnectDisabled: t.state === "connecting",
        reconnectBusy: t.state === "reconnecting",
        dotState: t.state === "ended" ? "done" : "live",
        dotLabel,
        jump: !down && t.scrolledUp,
        jumpCount: t.newLines > 999 ? "999+" : t.newLines > 0 ? String(t.newLines) : "",
        jumpLabel: t.newLines > 0 ? `Jump to latest, ${t.newLines} new lines` : "Jump to latest",
        groupLabel: run ? "Run terminal tools" : "Terminal tools",
      };
    },
    /** The strip at the top of the Agent pane when there is no live terminal: why, and Resume when it would be accepted. */
    get paneNote() {
      if (!this.showingAgent) return null;
      const mounted = this.terminalPanes.length > 0;
      const status = mounted ? this.paneTerminal : null;
      if (mounted && status.state !== "ended") return null;
      const code = status?.exitCode ?? null;
      const text =
        this.selected.terminalNote ||
        `This session ended. claude exited${code ? ` with code ${code}` : ""}. The screen below is its last, read-only.`;
      return {
        text,
        failed: this.selected.state === "failed" || (code !== null && code !== 0),
        resumable: this.canResumeSelected,
      };
    },
    /** The note with no terminal under it: an ended session opened, one in the TUI, a headless one. */
    get noteOnly() {
      return this.terminalPanes.length === 0 ? this.paneNote : null;
    },
    /** The agent waits on the person: the Agent tab's dot and the terminal's amber edge. */
    get attention() {
      return this.showingSession && this.selected.state === "waiting";
    },
    /** Focus mode is on, and there is a terminal's Exit focus to leave it by. */
    get focusActive() {
      return this.focusMode && this.paneTools !== null;
    },
    get textSizes() {
      return TEXT_SIZES.map((s) => ({ ...s, value: String(s.px), checked: s.px === this.textSize }));
    },
    get textSizeLabel() {
      return TEXT_SIZES.find((s) => s.px === this.textSize)?.label ?? "Default";
    },
    get shortId() {
      return this.selectedId ? this.selectedId.slice(0, 8) : "";
    },
    /** The head's meta: who runs it and since when. */
    get who() {
      return this.selected ? `${this.selected.agent} · ${this.selected.updated}` : "";
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
      return [{ key: `${this.selected.id}:${live ? "live" : "ended"}`, sessionId: this.selected.id, live, designHref: taskDesignHref(this.projectId, this.ticketId) }];
    },
    get designBadge() {
      return this.unseenArtifacts > 0 ? String(this.unseenArtifacts) : "";
    },
    get designTabSelected() {
      return this.detailTab === "design";
    },
    /** The selected session is the architect or a delegate: it has a conversation to show. */
    get hasConversation() {
      return this.showingSession && this.selectedRole() !== "";
    },
    get showingConversation() {
      return this.hasConversation && this.detailTab === "conversation";
    },
    /**
     * The selected delegate's status-check bar, keyed on what a person can
     * change, so a change from anywhere mounts a fresh one.
     */
    get statusCheckBars() {
      const session = this.showingSession ? this.sessions.find((s) => s.id === this.selectedId) : null;
      const check = session?.statusCheck;
      if (!check) return [];
      const view = statusCheckView(check, this.now);
      return [
        {
          key: `${session.id}:${check.state}:${check.everyMinutes}`,
          sessionId: session.id,
          check,
          remembered: this.lastIntervals[session.id] ?? 0,
          state: view.state,
          detail: view.detail,
          next: view.next,
          last: view.last,
          meta: check.state === "active" ? `· checks ${shortEvery(check.everyMinutes)}` : `· ${view.short}`,
        },
      ];
    },
    get conversationTabSelected() {
      return this.detailTab === "conversation";
    },
    get conversationBadge() {
      return this.unseenMessages > 0 ? String(this.unseenMessages) : "";
    },
    /** The Conversation tab's panel, keyed on the session, with what it needs to name people and links. */
    get conversationPanels() {
      if (!this.showingConversation) return [];
      const isArchitect = this.selectedRole() === "architect";
      const delegates = isArchitect
        ? this.sessions.filter((s) => s.role === "delegate").map((s) => delegateOption(s, this.agentNames))
        : [];
      return [
        {
          key: this.selectedId,
          sessionId: this.selectedId,
          isArchitect,
          delegates,
          sessions: this.sessions,
          agentNames: this.agentNames,
          documentTitles: this.documentTitles,
          projectId: this.projectId,
          ticketId: this.ticketId,
        },
      ];
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
        this.documentTitles = seed.documentTitles;
        this.sessions = seed.sessions.filter(ofTask(this.ticketId));
        for (const s of this.sessions) this.remember(s.statusCheck);
      } catch (err) {
        this.error = describeError(err);
        return;
      }
      const asked = new URLSearchParams(location?.search ?? "").get("session");
      // A link to a session opens it: on a narrow screen its detail, not the list.
      this.detailOpen = this.sessions.some((s) => s.id === asked);
      this.selectedId = this.detailOpen ? asked : (group(this.sessions)[0]?.sessions[0]?.id ?? null);

      unfollow = gateway.follow(
        this.projectId,
        (change) => this.apply(change),
        (status) => this.feedStatus(status),
      );
      this.followChannel();
      ticker = setInterval(() => (this.now = clock.now()), TICK_MS);

      // Alpine has rendered the live list; drop the server-rendered copy.
      this.$nextTick(() => {
        for (const node of this.$el.querySelectorAll("[data-ssr]")) node.remove();
        this.ready = true;
      });
    },

    destroy() {
      unfollow();
      unfollowChannel?.();
      clearInterval(ticker);
    },

    // ── the architect channel ────────────────────────────────────────────
    /** The task has an architect: its sessions lead the list and talk over the channel. */
    get hasArchitect() {
      return this.sessions.some((s) => s.role === "architect");
    },

    /**
     * Once the task has an architect (on load, or when one starts), load its
     * messages and follow new ones. A task without one never asks.
     */
    followChannel() {
      if (!channel || unfollowChannel || !this.hasArchitect) return;
      unfollowChannel = channel.follow(
        this.projectId,
        (event) => this.channelEvent(event),
        (status) => status === FeedStatus.RESYNCED && this.loadMessages(newestAt(channelStore.messages)),
      );
      this.loadMessages(null);
    },

    /** Loads the task's messages, or only those after since (after a resync), into the store. */
    async loadMessages(since) {
      try {
        const list = await channel.listMessages({ ticketId: this.ticketId, since });
        channelStore.messages = mergeMessages(channelStore.messages, list);
        channelStore.loaded = true;
      } catch (err) {
        this.error = describeError(err);
      }
    },

    /** @param {import("../../domain/ports.js").ChannelEvent} event */
    channelEvent({ message }) {
      if (message.taskId !== this.ticketId) return;
      const arriving = !channelStore.messages.some((m) => m.id === message.id);
      channelStore.messages = upsertMessage(channelStore.messages, message);
      if (!arriving) return; // the same message, now delivered
      this.announcement = announceMessage(message, { sessions: this.sessions, agentNames: this.agentNames });
      const role = this.selectedRole();
      const concernsSelected = role === "architect" || (role === "delegate" && (message.fromSessionId === this.selectedId || message.toSessionId === this.selectedId));
      if (concernsSelected && !this.showingConversation) this.unseenMessages += 1;
    },

    // ── feed ─────────────────────────────────────────────────────────────
    apply(change) {
      const before = change.kind === "upsert" ? this.sessions.find((s) => s.id === change.session.id) : undefined;
      const arriving = change.kind === "upsert" && !before;
      if (before && isTerminal(before) && !isTerminal(change.session)) this.resumed(change.session);
      this.sessions = applyChange(this.sessions, change, ofTask(this.ticketId));
      if (change.kind === "upsert") this.remember(change.session.statusCheck);
      if (arriving && this.sessions.some((s) => s.id === change.session.id)) this.arrived(change.session);
      this.now = clock.now();
      if (change.kind === "deleted" && change.id === this.selectedId) this.selectedId = null;
      // The session's Design panel follows its own stream; tell it the session is over.
      if (change.kind === "upsert" && isTerminal(change.session)) this.$dispatch("session-ended", { id: change.session.id });
      this.followChannel();
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
    /** The person opened the Agent tab: its terminal takes the focus once mounted. */
    showAgent() {
      this.detailTab = "agent";
      this.$nextTick(() => this.$dispatch("terminal-focus-requested"));
    },
    showApp() {
      this.detailTab = "app";
    },
    showDesign() {
      this.detailTab = "design";
      this.unseenArtifacts = 0;
    },
    showConversation() {
      this.detailTab = "conversation";
      this.unseenMessages = 0;
    },

    /** The selected session's role on the task; "" when none is selected. */
    /** Another part of the page (the review band) has something to say in the live region. */
    announce(event) {
      this.announcement = event.detail.text;
    },

    /** @param {CustomEvent<{ check: import("../../domain/channel.js").StatusCheck }>} event */
    statusCheckChanged(event) {
      this.patchCheck(event.detail.check);
    },

    /** A delegate's loop changed: its row and header follow before the server's session change does. */
    patchCheck(check) {
      this.sessions = this.sessions.map((s) => (s.id === check.delegateSessionId ? Object.freeze({ ...s, statusCheck: check }) : s));
      this.remember(check);
    },

    remember(check) {
      if (check?.everyMinutes > 0) this.lastIntervals = { ...this.lastIntervals, [check.delegateSessionId]: check.everyMinutes };
    },

    selectedRole() {
      return this.sessions.find((s) => s.id === this.selectedId)?.role ?? "";
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
      if (id !== this.selectedId) {
        this.unseenArtifacts = 0;
        this.unseenMessages = 0;
      }
      this.selectedId = id;
      // A peer has no conversation: fall back to its terminal.
      if (this.detailTab === "conversation" && this.selectedRole() === "") this.detailTab = "agent";
      this.creating = false;
      if (this.keptTerminalId !== id) this.keptTerminalId = null;
    },

    /** The person opened a session (a click or tap on its row): on a narrow screen its detail replaces the list. */
    open(id) {
      this.select(id);
      this.detailOpen = true;
    },
    /** Enter on the list opens the selected session. */
    openSelected() {
      if (this.selectedId) this.detailOpen = true;
    },
    /** Back to the list, which takes the focus the Sessions button had. */
    closeDetail() {
      this.detailOpen = false;
      this.$nextTick(() => this.$refs.list?.focus());
    },
    /** data-detail on the split view: present while the detail is open. */
    get detailOpenAttr() {
      return this.detailOpen ? "" : null;
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
      this.select(order[next]);
    },

    // New session: the form is its own component (sessionsNewSession); it
    // reports back with session-created or new-session-cancelled.
    startCreating() {
      if (this.cannotCreate) return;
      this.creating = true;
      this.detailOpen = true;
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

    // Delete: asked in the confirm on ⋯ (session-delete), then removed. The
    // head goes with the session, so the focus goes back to the list.
    async deleteSelected() {
      const id = this.selectedId;
      if (!id || this.deleting) return;
      this.deleting = true;
      try {
        await gateway.remove(id);
        this.sessions = applyChange(this.sessions, { kind: "deleted", id });
        if (this.selectedId === id) this.selectedId = null;
        this.$nextTick(() => this.$refs.list?.focus());
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
        if (this.keptTerminalId === id) this.keptTerminalId = null;
        this.terminalEpochs = { ...this.terminalEpochs, [id]: (this.terminalEpochs[id] ?? 0) + 1 };
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

    // ── the session screen: tab-row tools, text size, focus mode, ⋯ ──────
    /** @param {CustomEvent<import("../components/terminal.js").TerminalStatus>} event */
    terminalStatus(event) {
      const status = event.detail;
      if (status.kind === "run") {
        this.runStatus = status;
        return;
      }
      const before = this.terminalStatuses[status.id];
      if (status.newLines > 0 && !(before?.newLines > 0)) this.announcement = "New output below";
      this.terminalStatuses = { ...this.terminalStatuses, [status.id]: status };
      if (status.state === "ended" && status.id === this.selectedId && this.showingAgent) this.keptTerminalId = status.id;
    },
    /** @param {CustomEvent<{ id: string }>} event  the App panel's run on screen ("" for none) */
    runShown(event) {
      this.shownRunId = event.detail?.id ?? "";
    },
    requestReconnect() {
      const t = this.paneTools;
      if (t) this.$dispatch("terminal-reconnect-requested", { id: t.id });
    },
    requestJump() {
      const t = this.paneTools;
      if (t) this.$dispatch("terminal-jump-requested", { id: t.id });
    },
    /** @param {number | string} px */
    setTextSize(px) {
      const size = Number(px);
      if (!TEXT_SIZES.some((s) => s.px === size)) return;
      this.textSize = size;
      prefs.setTextSize(size);
      this.$dispatch("terminal-text-size", { px: size });
    },
    cycleTextSize() {
      this.setTextSize(nextTextSize(this.textSize));
    },
    toggleFocus() {
      this.focusMode = !this.focusMode;
      prefs.setFocus(this.focusMode);
    },
    /** Puts focus mode on the shell (x-effect): only while a terminal's tools, and so its Exit focus, show. */
    applyFocus() {
      shell.setFocus(this.focusActive);
    },
    async copySessionId() {
      const id = this.selectedId;
      if (!id) return;
      try {
        await clipboard.writeText(id);
        this.announcement = "Session id copied";
      } catch {
        this.announcement = `Could not copy. The session id is ${id}`;
      }
    },
    /**
     * An item of a menu in the session screen (they bubble up here): the head's
     * ⋯ (copy-id, ask-delete) and the tab row's text size (text-size, with px).
     * @param {CustomEvent<{ action: string, value?: string }>} event  its target is the menu
     */
    menuSelect(event) {
      const { action, value } = event.detail ?? {};
      if (action === "copy-id") return this.copySessionId();
      if (action === "text-size") return this.setTextSize(value);
      if (action === "ask-delete") this.$dispatch("dialog-open", { id: "session-delete", anchor: event.target?.querySelector?.(":scope > button") });
    },
    /**
     * On a phone, ⋯ opens the session sheet, not the menu: caught on the way
     * down (the head listens in the capture phase), before the menu sees the
     * click or the key that would open it.
     * @param {Event} event
     */
    moreCapture(event) {
      const trigger = /** @type {HTMLElement | null} */ (event.target)?.closest?.("#session-more");
      if (!trigger || !isPhone()) return;
      if (event.type === "keydown" && !["Enter", " ", "ArrowDown", "ArrowUp"].includes(/** @type {KeyboardEvent} */ (event).key)) return;
      event.preventDefault();
      event.stopPropagation();
      this.$dispatch("dialog-open", { id: "session-sheet", anchor: trigger });
    },
    /** @param {CustomEvent<{ id: string, action: string }>} event  a button in the confirm or the sheet (popupDialog's act) */
    dialogAction(event) {
      const { id, action } = event.detail ?? {};
      if (id !== "session-delete" && id !== "session-sheet") return;
      return this.sessionAction({ detail: { action } });
    },
    /** @param {{ detail?: { action: string } }} event */
    async sessionAction(event) {
      switch (event.detail?.action) {
        case "delete":
          return this.deleteSelected();
        case "ask-delete":
          // From the sheet: the confirm opens where the sheet was, anchored to ⋯.
          return this.$dispatch("dialog-open", { id: "session-delete", anchor: globalThis.document?.getElementById("session-more") });
        case "stop":
          return this.stopSelected();
        case "copy-id":
          return this.copySessionId();
        case "text-size":
          return this.cycleTextSize();
        case "resume":
          return this.resume();
      }
    },
  };
};

/** An interval, short: 20m, 1h. */
function shortEvery(minutes) {
  return minutes < 60 || minutes % 60 ? `${minutes}m` : `${minutes / 60}h`;
}

/** How long ago a terminal lost its connection, for the offline word. */
function lostWords(lostAt, now) {
  if (lostAt === null || lostAt === undefined) return "just now";
  const seconds = Math.max(0, Math.round((now.getTime() - lostAt) / 1000));
  if (seconds < 60) return "just now";
  const minutes = Math.round(seconds / 60);
  return minutes < 60 ? `${minutes}m ago` : `${Math.round(minutes / 60)}h ago`;
}

