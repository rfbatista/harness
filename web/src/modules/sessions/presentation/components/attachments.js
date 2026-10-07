// What the project's design library and a task's design page share: following
// the project feed for artifact changes, attaching and detaching project
// assets, and the in-flight guard. The feed echoes the person's own action,
// sometimes before its POST answers (the echo beats the answer); an echo for
// an asset with an action in flight is applied quietly, and anything else
// someone did (an agent, another tab) is announced.
//
//   compose(artifactBrowsing(clock), picker(), attachments({ artifacts, setTimeout }), {
//     keeps(artifact) { … },          does the asset belong on this list?
//     taskTitle(ticketId) { … },      a task's title, for announcements
//     load() { … },                    the list from the server again
//   })

import { FeedStatus } from "../../../../shared/domain/feed.js";
import { describeError } from "../../../../shared/presentation/errors.js";
import { feedStatusView } from "../../../../shared/presentation/feedStatus.js";
import { applyProjectChange } from "../../domain/artifact.js";
import { artifactTitle } from "../artifactView.js";

/** How long a card that arrived over the feed stays highlighted. */
export const FRESH_MS = 8_000;

/**
 * @param {{
 *   artifacts: import("../../domain/ports.js").ArtifactGateway,
 *   setTimeout?: typeof globalThis.setTimeout,
 * }} deps
 */
export function attachments({ artifacts, setTimeout = globalThis.setTimeout.bind(globalThis) }) {
  return {
    error: null,
    feed: FeedStatus.CONNECTING,
    followsFeed: true,
    /** Read out by a polite live region. */
    announcement: "",
    /** Assets with an action of the person's in flight: one at a time each. */
    pendingIds: [],

    get feedState() {
      return feedStatusView(this.feed).state;
    },
    get feedWord() {
      return feedStatusView(this.feed).label;
    },
    isPending(id) {
      return this.pendingIds.includes(id);
    },
    get currentPending() {
      return this.isPending(this.selectedId);
    },

    // ── the list's own rules (a component overrides these) ──────────────
    keeps(_artifact) {
      return true;
    },
    taskTitle(_ticketId) {
      return "a task";
    },

    // ── the person's actions ─────────────────────────────────────────────
    attachTo(artifactId, ticketId) {
      return this.act(artifactId, () => artifacts.attach(artifactId, ticketId));
    },
    detachFrom(artifactId, ticketId) {
      return this.act(artifactId, () => artifacts.detach(artifactId, ticketId));
    },

    /**
     * Runs one action on an asset, unless one is already in flight for it.
     * The answer is applied unless the list already holds something newer; on
     * failure the error shows and the list reloads, so it matches the server.
     * @param {string} id @param {() => Promise<import("../../domain/artifact.js").Artifact | void>} fn
     * @returns {Promise<boolean>} whether it succeeded
     */
    async act(id, fn) {
      if (this.isPending(id)) return false;
      this.pendingIds = [...this.pendingIds, id];
      this.error = null;
      try {
        const answer = await fn();
        if (answer) this.applyChange({ kind: "changed", artifact: answer });
        return true;
      } catch (err) {
        await this.load();
        this.error = describeError(err); // after the reload, which clears the last error
        return false;
      } finally {
        this.pendingIds = this.pendingIds.filter((x) => x !== id);
      }
    },

    // ── the feed ─────────────────────────────────────────────────────────
    /** @param {import("../../domain/ports.js").ProjectArtifactChange} change */
    onProjectChange(change) {
      const id = change.kind === "deleted" ? change.id : change.artifact.id;
      const before = this.artifacts.find((a) => a.id === id);
      this.applyChange(change);
      if (this.isPending(id)) return; // the person's own action: its echo is no news
      const after = this.artifacts.find((a) => a.id === id);
      if (after && !before) this.markFresh(id);
      const said = this.describeChange(change, before, after);
      if (said) this.announcement = said;
    },

    onFeedStatus(status) {
      this.feed = status;
      if (status === FeedStatus.RESYNCED) this.load();
    },

    /** Applies a change by the list's rule; a dropped selection moves to its neighbour. */
    applyChange(change) {
      const at = this.artifacts.findIndex((a) => a.id === this.selectedId);
      this.artifacts = applyProjectChange(this.artifacts, change, (a) => this.keeps(a));
      if (this.selectedId && !this.artifacts.some((a) => a.id === this.selectedId)) {
        this.selectedId = this.artifacts[Math.min(Math.max(at, 0), this.artifacts.length - 1)]?.id ?? "";
      } else if (!this.selectedId) {
        this.selectedId = this.artifacts[0]?.id ?? "";
      }
    },

    markFresh(id) {
      this.freshIds = [...this.freshIds.filter((x) => x !== id), id];
      setTimeout(() => {
        this.freshIds = this.freshIds.filter((x) => x !== id);
      }, FRESH_MS);
    },

    /** What someone else's change did to this list, in words; "" when nothing a person would notice. */
    describeChange(change, before, after) {
      const title = artifactTitle(after ?? before ?? { title: "", path: "", url: "" });
      if (!before && !after) return "";
      if (before && !after) {
        if (change.kind === "deleted") return `Deleted: ${title}`;
        // A task page loses an asset by a move back to its task, or by a detach from this task.
        return change.artifact.scope === "task" ? `Moved back to its task: ${title}` : `Detached from this task: ${title}`;
      }
      if (!before) return `New here: ${title}`;
      if (after.revision > before.revision) return `Updated: ${title}, revision ${after.revision}`;
      const added = after.attachedTicketIds.filter((t) => !before.attachedTicketIds.includes(t));
      const removed = before.attachedTicketIds.filter((t) => !after.attachedTicketIds.includes(t));
      if (added.length) return `Attached to ${added.map((t) => this.taskTitle(t)).join(", ")}: ${title}`;
      if (removed.length) return `Detached from ${removed.map((t) => this.taskTitle(t)).join(", ")}: ${title}`;
      return "";
    },

    dismissError() {
      this.error = null;
    },
  };
}
