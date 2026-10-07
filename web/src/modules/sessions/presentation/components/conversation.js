// A session's Conversation tab: the task messages between the architect and
// its delegates, oldest first, live from the page's channel store. On the
// architect it shows every message and can narrow to one delegate; on a
// delegate it shows that delegate's thread with the architect. Messages are
// agents' text: the markup binds them with x-text, never as HTML.
//
//   <template x-for="panel in conversationPanels" x-bind:key="panel.key">
//     <section x-data="sessionsConversation(panel)"> …
//
// The page counts and announces arrivals (it sees them behind other tabs);
// this tab only shows them, and keeps the newest in view while the reader is
// at the bottom.

import { threadOf } from "../../domain/channel.js";
import { messageView } from "../channelView.js";

const TICK_MS = 30_000;
/** How close to the bottom still counts as reading the newest. */
const NEAR_BOTTOM_PX = 48;

/**
 * @param {{
 *   artifacts: import("../../domain/ports.js").ArtifactGateway,
 *   channelStore: import("../pages/sessionsPage.js").ChannelStore,
 *   clock: import("../../../../shared/infrastructure/clock.js").Clock,
 * }} deps
 */
export const conversation = ({ artifacts, channelStore, clock }) => (panel = {}) => {
  let ticker = null;
  /** Sessions whose artifacts were already asked for. */
  const asked = new Set();

  return {
    sessionId: panel.sessionId ?? "",
    isArchitect: panel.isArchitect === true,
    /** On the architect: "" for every delegate, or one delegate's id. */
    filter: "",
    /** Long messages the reader unfolded. */
    expandedIds: [],
    /** Artifacts named so far: id → { title, href }. */
    artifactTitles: {},
    now: clock.now(),

    // ── what the markup binds ────────────────────────────────────────────
    get delegates() {
      return panel.delegates ?? [];
    },
    get showsFilter() {
      return this.isArchitect && this.delegates.length > 0;
    },
    /** @returns {import("../../domain/channel.js").TaskMessage[]} */
    get messages() {
      const all = channelStore.messages;
      const mine = this.isArchitect ? all : threadOf(all, this.sessionId);
      return this.filter ? threadOf(mine, this.filter) : mine;
    },
    get count() {
      return this.messages.length;
    },
    get views() {
      const ctx = {
        sessions: panel.sessions ?? [],
        agentNames: panel.agentNames ?? {},
        documentTitles: panel.documentTitles ?? {},
        artifactTitles: this.artifactTitles,
        projectId: panel.projectId ?? "",
        ticketId: panel.ticketId ?? "",
        now: this.now,
        messages: channelStore.messages,
      };
      return this.messages.map((m) => messageView(m, ctx));
    },
    get loading() {
      return !channelStore.loaded;
    },
    get isEmpty() {
      return channelStore.loaded && this.messages.length === 0;
    },

    clamped(view) {
      return view.long && !this.expandedIds.includes(view.id);
    },
    expanded(view) {
      return this.expandedIds.includes(view.id);
    },
    toggleWord(view) {
      return this.expanded(view) ? "Show less" : "Show all";
    },
    toggle(id) {
      this.expandedIds = this.expandedIds.includes(id) ? this.expandedIds.filter((x) => x !== id) : [...this.expandedIds, id];
    },

    // ── lifecycle ────────────────────────────────────────────────────────
    init() {
      ticker = setInterval(() => (this.now = clock.now()), TICK_MS);
      this.nameArtifacts();
      this.$nextTick(() => this.scrollToEnd());
      // A new message: name its artifacts, and follow it down if the reader was at the end.
      this.$watch("count", () => {
        const following = this.atEnd();
        this.nameArtifacts();
        this.now = clock.now();
        if (following) this.$nextTick(() => this.scrollToEnd());
      });
    },

    destroy() {
      clearInterval(ticker);
    },

    /**
     * Names the artifacts the shown messages point at, by listing each
     * sender's artifacts once. One that cannot be found keeps its short id.
     */
    async nameArtifacts() {
      const senders = new Set(this.messages.filter((m) => m.artifactIds.length > 0).map((m) => m.fromSessionId));
      for (const sessionId of senders) {
        if (asked.has(sessionId)) continue;
        asked.add(sessionId);
        try {
          const list = await artifacts.list(sessionId);
          const named = { ...this.artifactTitles };
          for (const a of list) named[a.id] = { title: a.title || a.path || "Artifact", href: a.src };
          this.artifactTitles = named;
        } catch {
          asked.delete(sessionId); // tried again with the next message
        }
      }
    },

    atEnd() {
      const log = this.$refs.log;
      return !log || log.scrollHeight - log.scrollTop - log.clientHeight < NEAR_BOTTOM_PX;
    },

    scrollToEnd() {
      const log = this.$refs.log;
      if (log) log.scrollTop = log.scrollHeight;
    },
  };
};
