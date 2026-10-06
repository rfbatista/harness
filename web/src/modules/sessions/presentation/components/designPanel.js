// A session's Design tab: what it published, newest first, with the selected
// artifact rendered beside the list and refreshed as the session re-publishes.
// Mounted once per session (the x-for key) and kept mounted across tab
// switches (x-show), so the stream stays open and the page can count publishes
// behind the Agent and App tabs. It tells the page with artifact-published.
//
//   <template x-for="panel in designPanels" x-bind:key="panel.key">
//     <section x-data="sessionsDesignPanel(panel)" x-show="showingDesign"> …

import { FeedStatus } from "../../../../shared/domain/feed.js";
import { describeError } from "../../../../shared/presentation/errors.js";
import { feedStatusView } from "../../../../shared/presentation/feedStatus.js";
import { applyPublish, isKnown } from "../../domain/artifact.js";
import { toCardView, toPreviewView } from "../artifactView.js";

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

  return {
    sessionId: panel.sessionId ?? "",
    /** The session is alive: its stream is worth following. */
    live: panel.live === true,
    /** @type {import("../../domain/artifact.js").Artifact[]} */
    artifacts: [],
    selectedId: "",
    /** The developer picked an artifact by hand; new publishes no longer take the preview. */
    chosen: false,
    ready: false,
    error: null,
    feed: FeedStatus.CONNECTING,
    ended: false,
    freshIds: [],
    now: clock.now(),

    // ── what the markup binds ────────────────────────────────────────────
    get cards() {
      const fresh = new Set(this.freshIds);
      return this.artifacts.map((a) => toCardView(a, { selectedId: this.selectedId, now: this.now, fresh: fresh.has(a.id) }));
    },
    get current() {
      const a = this.artifacts.find((x) => x.id === this.selectedId);
      return a ? toPreviewView(a, this.now) : null;
    },
    get hasCurrent() {
      return this.current !== null;
    },
    /** The preview as a one-item list keyed on id@revision, so a new revision remounts the element. */
    get frames() {
      return this.current ? [this.current] : [];
    },
    get currentKind() {
      return this.current?.kind ?? "";
    },
    get hasOpenHref() {
      return (this.current?.openHref ?? "") !== "";
    },
    get currentOpenHref() {
      return this.current?.openHref ?? "";
    },
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
      if (this.live) {
        unfollow = artifacts.follow(
          this.sessionId,
          (event) => this.onEvent(event),
          (status) => this.onStatus(status),
        );
      }
    },

    destroy() {
      unfollow();
    },

    async load() {
      try {
        this.artifacts = await artifacts.list(this.sessionId);
        this.now = clock.now();
        if (!this.artifacts.some((a) => a.id === this.selectedId)) {
          this.selectedId = this.artifacts[0]?.id ?? "";
        }
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
      const isNew = !isKnown(this.artifacts, artifact);
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

    // ── developer actions ────────────────────────────────────────────────
    select(id) {
      this.selectedId = id;
      this.chosen = true;
    },
    next() {
      this.step(1);
    },
    previous() {
      this.step(-1);
    },
    step(delta) {
      if (this.artifacts.length === 0) return;
      const at = this.artifacts.findIndex((a) => a.id === this.selectedId);
      const to = at === -1 ? 0 : Math.max(0, Math.min(this.artifacts.length - 1, at + delta));
      this.select(this.artifacts[to].id);
    },
    dismissError() {
      this.error = null;
    },
  };
};
