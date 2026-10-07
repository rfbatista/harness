// A session's Design tab: what it published, newest first, with the selected
// artifact rendered beside the list and refreshed as the session re-publishes.
// Mounted once per session (the x-for key) and kept mounted across tab
// switches (x-show), so the stream stays open and the page can count publishes
// behind the Agent and App tabs. It tells the page with artifact-published.
// The selected artifact moves between task and project from the bar; a move,
// the person's or an agent's, changes the card in place and is not a publish:
// an agent's is told with artifact-moved, the person's own not at all.
//
//   <template x-for="panel in designPanels" x-bind:key="panel.key">
//     <div x-show="showingDesign"><section x-data="sessionsDesignPanel(panel)"> …
//
// The x-show stays outside the panel's x-data: this scope has its own
// selectedId (an artifact), and a page getter evaluated from inside it would
// read that instead of the page's selected session.

import { FeedStatus } from "../../../../shared/domain/feed.js";
import { describeError } from "../../../../shared/presentation/errors.js";
import { feedStatusView } from "../../../../shared/presentation/feedStatus.js";
import { applyChange, applyPublish, isNewer, isPublish } from "../../domain/artifact.js";
import { artifactBrowsing, compose } from "./artifactBrowsing.js";

/** How long a card that arrived over the stream stays highlighted. */
export const FRESH_MS = 8_000;

const ENDED = { state: "done", label: "session ended" };

/**
 * @param {{
 *   artifacts: import("../../domain/ports.js").ArtifactGateway,
 *   clock: import("../../../../shared/infrastructure/clock.js").Clock,
 *   setTimeout?: typeof globalThis.setTimeout,
 * }} deps
 */
export const designPanel = ({ artifacts, clock, setTimeout = globalThis.setTimeout.bind(globalThis) }) => (panel = {}) => {
  let unfollow = () => {};
  let destroyed = false;

  return compose(artifactBrowsing(clock), {
    sessionId: panel.sessionId ?? "",
    /** The task's design assets page, where the project assets attached to the task are. */
    designHref: panel.designHref ?? "",
    /** The session is alive: its stream is worth following. */
    live: panel.live === true,
    ready: false,
    error: null,
    feed: FeedStatus.CONNECTING,
    ended: false,
    moving: false,
    /** The artifact the person is moving: its echo may beat the answer, and is theirs, not news. */
    movingId: "",
    followsFeed: true,

    // ── what the markup binds (the rest: artifactBrowsing) ───────────────
    get isEmpty() {
      return this.ready && this.artifacts.length === 0;
    },
    get feedState() {
      return this.feedView().state;
    },
    get feedWord() {
      return this.feedView().label;
    },
    feedView() {
      return this.ended || !this.live ? ENDED : feedStatusView(this.feed);
    },

    // ── lifecycle ────────────────────────────────────────────────────────
    async init() {
      await this.load();
      // Unmounted while the list was loading (another session picked): no stream to open.
      if (this.live && !destroyed && !this.ended) {
        unfollow = artifacts.follow(
          this.sessionId,
          (event) => this.onEvent(event),
          (status) => this.onStatus(status),
        );
      }
    },

    destroy() {
      destroyed = true;
      unfollow();
    },

    async load() {
      try {
        this.artifacts = await artifacts.list(this.sessionId);
        this.now = clock.now();
        this.keepSelection();
      } catch (err) {
        this.error = describeError(err);
      } finally {
        this.ready = true;
      }
    },

    // ── stream ───────────────────────────────────────────────────────────
    onEvent(event) {
      if (event.kind === "ended") {
        this.ended = true;
        unfollow();
        unfollow = () => {};
        return;
      }
      const { artifact } = event;
      // The stream replays its history on every connect, and echoes the
      // person's own move: what the list already holds (or something staler)
      // is nothing new to show or announce.
      const known = this.artifacts.find((a) => a.id === artifact.id);
      if (!isNewer(known, artifact)) return;
      if (!isPublish(this.artifacts, artifact)) {
        this.artifacts = applyChange(this.artifacts, artifact);
        if (artifact.id !== this.movingId) this.$dispatch("artifact-moved", { artifact });
        return;
      }
      const isNew = !known;
      this.artifacts = applyPublish(this.artifacts, artifact);
      this.now = clock.now();
      if ((isNew && !this.chosen) || !this.selectedId) this.selectedId = artifact.id;
      this.freshIds = [...this.freshIds.filter((id) => id !== artifact.id), artifact.id];
      setTimeout(() => {
        this.freshIds = this.freshIds.filter((id) => id !== artifact.id);
      }, FRESH_MS);
      this.$dispatch("artifact-published", { artifact, isNew });
    },

    onStatus(status) {
      this.feed = status;
      if (status === FeedStatus.RESYNCED) this.load();
    },

    /**
     * The page saw this session turn terminal on the project feed; the same
     * end as a done event, for a session whose stream never says it.
     * @param {CustomEvent<{ id: string }>} event
     */
    sessionEnded(event) {
      if (event.detail?.id === this.sessionId) this.onEvent({ kind: "ended" });
    },

    // ── developer actions ────────────────────────────────────────────────
    /** Moves the selected artifact to the project, or back to its task. */
    async move() {
      const target = this.current;
      if (this.moving || !target?.canMove) return;
      this.moving = true;
      this.movingId = target.id;
      this.error = null;
      try {
        const moved = await artifacts.setScope(target.id, target.moveTarget);
        const known = this.artifacts.find((a) => a.id === moved.id);
        if (isNewer(known, moved)) this.artifacts = applyChange(this.artifacts, moved);
      } catch (err) {
        this.error = describeError(err);
      } finally {
        this.moving = false;
        this.movingId = "";
      }
    },
    dismissError() {
      this.error = null;
    },
  });
};
