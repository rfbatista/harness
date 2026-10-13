// A session's History tab: its saved conversation, read-only, newest at the
// bottom (the contract "Harness server ↔ Web UI — agent session history",
// T1–T6). It is what the terminal no longer holds: claude draws on the
// alternate screen, which keeps no scrollback.
//
//   <template x-for="panel in historyPanels" x-bind:key="panel.key">
//     <section class="[ frame ]" x-data="sessionsHistory(panel)"> …
//       <div class="[ transcript ]" data-scroll x-ref="log" x-on:scroll="onScroll"> …
//
// Mounted only while the tab is in front, keyed on the session and whether
// it runs. It opens on the newest page; scrolling near the top shows older
// lines, loading them a page at a time. While the session runs, the page is
// visible and the tab shows, it catches up every 2 s (T5): a reader at the
// bottom follows, one who scrolled up stays put and is offered Jump to
// latest. Thousands of entries stay smooth: every loaded entry is kept, but
// only a window of them is rendered, moved as the reader nears its edges,
// keeping the line they look at where it was.

import { describeError } from "../../../../shared/presentation/errors.js";
import { isTranscriptMissing, TRANSCRIPT_PAGE } from "../../domain/transcript.js";
import { historyLine } from "../historyView.js";

/** The catch-up interval while the session runs (T5: no more often than every 2 s). */
export const POLL_MS = 2_000;
/** At most this many lines are rendered; the window moves by CHUNK. */
export const MAX_LINES = 600;
export const CHUNK = 200;
/** How close to the bottom still counts as reading the newest. */
const NEAR_BOTTOM_PX = 48;
/** How close to an edge of the rendered window moves it (or loads older lines). */
const EDGE_PX = 240;
/** Catching up stops after this many pages in one go; the next poll goes on. */
const MAX_PAGES_PER_POLL = 10;

/**
 * @param {{
 *   transcripts: import("../../domain/ports.js").TranscriptGateway,
 *   timers?: { setTimeout: typeof globalThis.setTimeout, clearTimeout: typeof globalThis.clearTimeout },
 *   pageVisible?: () => boolean,  the browser tab is in front
 * }} deps
 */
export const history = ({ transcripts, timers = globalThis, pageVisible = () => globalThis.document?.visibilityState !== "hidden" }) => (panel = {}) => {
  let pollTimer = 0;
  let busy = false;
  let gone = false;
  /** Lines already built, by cursor: entries never change once written. */
  const built = new Map();

  return {
    sessionId: panel.sessionId ?? "",
    live: panel.live === true,
    /** loading → ready, or missing (no saved conversation yet), or failed (the first page did not load) */
    state: "loading",
    /** Why the last request failed, while it shows. */
    error: "",
    /** @type {import("../../domain/transcript.js").TranscriptEntry[]} every loaded entry, oldest first */
    entries: [],
    /** The rendered window, entries[start, end). */
    start: 0,
    end: 0,
    hasOlder: false,
    loadingOlder: false,
    /** The reader is at the bottom of the newest lines: new ones scroll in. */
    following: true,
    /** Entries that arrived below while the reader was scrolled up. */
    newCount: 0,
    expandedIds: [],

    // ── what the markup binds ────────────────────────────────────────────
    get lines() {
      return this.entries.slice(this.start, this.end).map((e) => {
        let line = built.get(e.cursor);
        if (!line) built.set(e.cursor, (line = historyLine(e)));
        return line;
      });
    },
    get showsLog() {
      return this.state === "ready";
    },
    get isLoading() {
      return this.state === "loading";
    },
    get isMissing() {
      return this.state === "missing";
    },
    /** The strip above the log: a failed first page, catch-up or older page. */
    get showsError() {
      return this.error !== "";
    },
    /** More lines above the rendered ones, loaded or not. */
    get canLoadOlder() {
      return this.start > 0 || this.hasOlder;
    },
    get atStart() {
      return !this.canLoadOlder;
    },
    get showsJump() {
      return this.showsLog && !this.following;
    },
    get jumpLabel() {
      return this.newCount > 0 ? `${this.newCount} new · Jump to latest` : "Jump to latest";
    },
    expanded(line) {
      return this.expandedIds.includes(line.cursor);
    },
    toggle(line) {
      const id = line.cursor;
      this.expandedIds = this.expandedIds.includes(id) ? this.expandedIds.filter((x) => x !== id) : [...this.expandedIds, id];
    },

    // ── lifecycle ────────────────────────────────────────────────────────
    init() {
      this.load().then(() => this.schedule());
    },

    destroy() {
      gone = true;
      timers.clearTimeout(pollTimer);
    },

    /** The newest page, from scratch: on open, and while there is no saved conversation yet. */
    async load() {
      busy = true;
      try {
        const page = await transcripts.page(this.sessionId, { limit: TRANSCRIPT_PAGE });
        if (gone) return;
        this.entries = page.entries;
        this.hasOlder = page.hasOlder;
        this.end = page.entries.length;
        this.start = Math.max(0, this.end - MAX_LINES);
        this.following = true;
        this.newCount = 0;
        this.error = "";
        this.state = "ready";
        this.$nextTick(() => this.scrollToEnd());
      } catch (err) {
        if (gone) return;
        if (isTranscriptMissing(err)) {
          this.state = "missing";
          this.error = "";
        } else {
          this.state = "failed";
          this.error = describeError(err).message;
        }
      } finally {
        busy = false;
      }
    },

    /** Next catch-up, while the session runs. */
    schedule() {
      if (gone || !this.live) return;
      timers.clearTimeout(pollTimer);
      pollTimer = timers.setTimeout(() => this.poll(), POLL_MS);
    },

    async poll() {
      if (gone) return;
      if (!busy && pageVisible()) {
        if (this.state === "ready") await this.catchUp();
        else await this.load();
      }
      this.schedule();
    },

    /** What the CLI wrote since the newest loaded entry. */
    async catchUp() {
      if (this.entries.length === 0) return this.load();
      busy = true;
      try {
        for (let i = 0; i < MAX_PAGES_PER_POLL; i++) {
          const page = await transcripts.page(this.sessionId, { after: this.entries.at(-1).cursor, limit: TRANSCRIPT_PAGE });
          if (gone) return;
          this.error = "";
          if (page.entries.length > 0) this.append(page.entries);
          if (!page.hasNewer) break;
        }
      } catch (err) {
        if (!gone) this.error = describeError(err).message;
      } finally {
        busy = false;
      }
    },

    append(added) {
      const windowAtEnd = this.end === this.entries.length;
      this.entries = [...this.entries, ...added];
      if (this.following) {
        this.end = this.entries.length;
        this.start = Math.max(this.start, this.end - MAX_LINES);
        this.$nextTick(() => this.scrollToEnd());
        return;
      }
      // Below a reader who scrolled up: they stay where they are.
      this.newCount += added.length;
      if (windowAtEnd) this.end = Math.min(this.entries.length, this.start + MAX_LINES);
    },

    // ── reading ──────────────────────────────────────────────────────────
    onScroll() {
      const log = this.$refs.log;
      if (!log) return;
      const fromBottom = log.scrollHeight - log.scrollTop - log.clientHeight;
      this.following = fromBottom < NEAR_BOTTOM_PX && this.end === this.entries.length;
      if (this.following) this.newCount = 0;
      if (log.scrollTop < EDGE_PX) this.older();
      else if (fromBottom < EDGE_PX && this.end < this.entries.length) this.later();
    },

    /** Lines above the window: already loaded ones first, then a page from the server. */
    async older() {
      if (this.loadingOlder) return;
      if (this.start > 0) {
        const kept = this.anchor();
        this.start = Math.max(0, this.start - CHUNK);
        this.end = Math.min(this.end, this.start + MAX_LINES);
        this.restore(kept);
        return;
      }
      if (!this.hasOlder || this.entries.length === 0) return;
      this.loadingOlder = true;
      try {
        const page = await transcripts.page(this.sessionId, { before: this.entries[0].cursor, limit: TRANSCRIPT_PAGE });
        if (gone) return;
        const kept = this.anchor();
        const n = page.entries.length;
        this.entries = [...page.entries, ...this.entries];
        this.hasOlder = page.hasOlder;
        this.end = Math.min(this.end + n, MAX_LINES);
        this.start = 0;
        this.error = "";
        this.restore(kept);
      } catch (err) {
        if (!gone) this.error = describeError(err).message;
      } finally {
        this.loadingOlder = false;
      }
    },

    /** Loaded lines below the window, as the reader scrolls back down. */
    later() {
      const kept = this.anchor();
      this.end = Math.min(this.entries.length, this.end + CHUNK);
      this.start = Math.max(this.start, this.end - MAX_LINES);
      this.restore(kept);
    },

    /** Back to the newest line; the focus stays where it is. */
    jump() {
      this.end = this.entries.length;
      this.start = Math.max(0, this.end - MAX_LINES);
      this.following = true;
      this.newCount = 0;
      this.$nextTick(() => this.scrollToEnd());
    },

    /** Retry after a failure: the first page again, or the catch-up. */
    retry() {
      this.error = "";
      if (this.state === "ready") return this.catchUp();
      this.state = "loading";
      return this.load();
    },

    scrollToEnd() {
      const log = this.$refs.log;
      if (log) log.scrollTop = log.scrollHeight;
    },

    /** The first line the reader can see, and how far below the log's top it is. */
    anchor() {
      const log = this.$refs.log;
      if (!log) return null;
      const top = log.getBoundingClientRect().top;
      for (const el of log.querySelectorAll("[data-cursor]")) {
        const box = el.getBoundingClientRect();
        if (box.bottom > top) return { cursor: el.dataset.cursor, offset: box.top - top };
      }
      return null;
    },

    /** After the window moved: that line back where it was. */
    restore(kept) {
      if (!kept) return;
      this.$nextTick(() => {
        const log = this.$refs.log;
        const el = log?.querySelector(`[data-cursor="${CSS.escape(kept.cursor)}"]`);
        if (!el) return;
        log.scrollTop += el.getBoundingClientRect().top - log.getBoundingClientRect().top - kept.offset;
      });
    },
  };
};
