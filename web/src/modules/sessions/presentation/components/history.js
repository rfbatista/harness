// A session's History tab: its saved conversation as a chat, read-only,
// newest at the bottom (the contract "Harness server ↔ Web UI — agent session
// history", T1–T7). It is what the terminal no longer holds: claude draws on the
// alternate screen, which keeps no scrollback.
//
//   <template x-for="panel in historyPanels" x-bind:key="panel.key">
//     <section class="[ frame ]" x-data="sessionsHistory(panel)"> …
//       <div class="[ chat ]" data-scroll role="log" x-on:scroll="onScroll"> …
//
// Mounted only while the tab is in front, keyed on the session and whether
// it runs. It opens on the newest page; scrolling near the top shows older
// lines, loading them a page at a time. While the session runs, the page is
// visible and the tab shows, it catches up every 2 s (T5): a reader at the
// bottom follows, one who scrolled up stays put and is offered Jump to
// latest. Thousands of entries stay smooth: every loaded entry is kept, but
// only a window of them is rendered, moved as the reader nears its edges,
// keeping the line they look at where it was. The reader can hide the tool
// calls (remembered per browser) and search what is loaded.

import { describeError } from "../../../../shared/presentation/errors.js";
import { isStaleCursor, isTranscriptMissing, TRANSCRIPT_PAGE } from "../../domain/transcript.js";
import { chatItems, historyEntry, isWorking, matches } from "../historyView.js";

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
/** Where the reader's choice to show tool calls is kept. */
export const TOOLS_KEY = "harness.history.showTools";
/** The CSS highlight that marks search matches. */
const HIGHLIGHT = "history-match";

/**
 * @param {{
 *   transcripts: import("../../domain/ports.js").TranscriptGateway,
 *   timers?: { setTimeout: typeof globalThis.setTimeout, clearTimeout: typeof globalThis.clearTimeout },
 *   pageVisible?: () => boolean,  the browser tab is in front
 *   storage?: Pick<Storage, "getItem" | "setItem"> | null,  where showTools is kept
 *   clipboard?: { writeText(text: string): Promise<void> } | null,
 * }} deps
 */
export const history = ({
  transcripts,
  timers = globalThis,
  pageVisible = () => globalThis.document?.visibilityState !== "hidden",
  storage = safeStorage(),
  clipboard = globalThis.navigator?.clipboard ?? null,
}) => (panel = {}) => {
  let pollTimer = 0;
  let busy = false;
  let gone = false;
  /**
   * The panel's element, kept from init: $refs and $el in a method resolve
   * from the element that called it, and Jump to latest is gone by the time
   * its scroll runs.
   */
  let root = null;
  const logEl = () => root?.querySelector("[role=log]") ?? null;
  /** Entries already built, by cursor: entries never change once written. */
  const built = new Map();
  const build = (e) => {
    let b = built.get(e.cursor);
    if (!b) built.set(e.cursor, (b = historyEntry(e)));
    return b;
  };

  return {
    sessionId: panel.sessionId ?? "",
    live: panel.live === true,
    /** Who the agent's messages are from. */
    agent: panel.agent || "Agent",
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
    /** Activity cards, calls and long messages the reader opened, by key. */
    expandedIds: [],
    /** The tool calls show between the messages. */
    showTools: storage?.getItem(TOOLS_KEY) !== "false",
    /** What the reader searches for, and which match is current (-1: none yet). */
    query: "",
    hit: -1,

    // ── what the markup binds ────────────────────────────────────────────
    /** The rendered window's entries. */
    get rendered() {
      return this.entries.slice(this.start, this.end).map(build);
    },
    get items() {
      return chatItems(this.rendered, { agent: this.agent, live: this.live && this.end === this.entries.length, showTools: this.showTools });
    },
    /** The agent is between its turns, below the newest message. */
    get working() {
      return this.showsLog && this.end === this.entries.length && isWorking(this.entries, this.live);
    },
    get hits() {
      return matches(this.entries.map(build), this.query, this.showTools);
    },
    get searchLabel() {
      if (this.query.trim() === "") return "";
      const n = this.hits.length;
      if (n === 0) return "No match";
      return this.hit < 0 ? `${n} found` : `${this.hit + 1} of ${n}`;
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
    /** An activity card, a call or a long message (by its key or cursor) is open. */
    expanded(id) {
      return this.expandedIds.includes(id);
    },
    toggle(id) {
      this.expandedIds = this.expandedIds.includes(id) ? this.expandedIds.filter((x) => x !== id) : [...this.expandedIds, id];
      this.repaint();
    },
    get workingLabel() {
      return `${this.agent} is working…`;
    },
    moreLabel(part) {
      return this.expandedIds.includes(part.cursor) ? "Show less" : "Show all";
    },
    /** A long message folded until shown in full. */
    clamped(part) {
      return part.long && !this.expandedIds.includes(part.cursor);
    },

    toggleTools() {
      this.showTools = !this.showTools;
      storage?.setItem(TOOLS_KEY, String(this.showTools));
      this.hit = -1;
      this.repaint();
    },

    // ── search ───────────────────────────────────────────────────────────
    onSearch() {
      this.hit = -1;
      this.repaint();
    },
    /** Enter: the next match; Shift+Enter: the one before; Escape: clear. */
    onSearchKey(event) {
      if (event.key === "Escape" && this.query !== "") {
        event.preventDefault();
        this.query = "";
        this.onSearch();
        return;
      }
      if (event.key !== "Enter") return;
      event.preventDefault();
      this.step(event.shiftKey ? -1 : 1);
    },
    /** Move to the next (1) or previous (-1) match: into the window, its card open, in view. */
    step(by) {
      const hits = this.hits;
      if (hits.length === 0) return;
      this.hit = this.hit < 0 ? (by > 0 ? 0 : hits.length - 1) : (this.hit + by + hits.length) % hits.length;
      const cursor = hits[this.hit];
      const at = this.entries.findIndex((e) => e.cursor === cursor);
      if (at < this.start || at >= this.end) {
        this.start = Math.max(0, Math.min(at - CHUNK, this.entries.length - MAX_LINES));
        this.end = Math.min(this.entries.length, this.start + MAX_LINES);
        this.following = false;
      }
      const card = this.items.find((it) => it.type === "activity" && it.calls.some((c) => c.cursors.includes(cursor)));
      if (card) {
        const call = card.calls.find((c) => c.cursors.includes(cursor));
        this.expandedIds = [...new Set([...this.expandedIds, card.key, call.key])];
      }
      this.repaint(() => {
        const el = logEl()?.querySelector(`[data-cursor="${CSS.escape(cursor)}"], [data-cursors~="${CSS.escape(cursor)}"]`);
        el?.scrollIntoView({ block: "center" });
      });
    },

    /**
     * A message's Markdown, as the server rendered it in safe mode (T7), into
     * its element, with its code blocks dressed. Its html never changes, so
     * it is placed once.
     */
    placeHtml(el, part) {
      if (el.dataset.placed === part.cursor) return;
      el.dataset.placed = part.cursor;
      el.innerHTML = part.html;
      decorateCode(el);
      if (this.query.trim() !== "") this.repaint();
    },

    // ── copying ──────────────────────────────────────────────────────────
    /** A message's Markdown, as written. */
    copyTurn(item, event) {
      return this.copy(item.parts.map((p) => p.text).join("\n\n"), event?.currentTarget);
    },
    /** Clicks inside rendered Markdown: a code block's Copy. */
    onLogClick(event) {
      const button = event.target?.closest?.("[data-copy-code]");
      if (!button) return;
      const code = button.closest(".codeblock")?.querySelector("code");
      if (code) this.copy(code.textContent, button);
    },
    async copy(text, button) {
      if (!clipboard) return;
      try {
        await clipboard.writeText(text);
      } catch {
        return;
      }
      if (!button) return;
      const label = button.textContent;
      button.textContent = "Copied";
      timers.setTimeout(() => {
        button.textContent = label;
      }, 1_200);
    },

    // ── lifecycle ────────────────────────────────────────────────────────
    init() {
      root = this.$el;
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
        this.repaint(() => this.scrollToEnd());
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
        if (gone) return;
        if (isStaleCursor(err)) await this.load();
        else this.error = describeError(err).message;
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
        this.repaint(() => this.scrollToEnd());
        return;
      }
      // Below a reader who scrolled up: they stay where they are.
      this.newCount += added.length;
      if (windowAtEnd) this.end = Math.min(this.entries.length, this.start + MAX_LINES);
      this.repaint();
    },

    // ── reading ──────────────────────────────────────────────────────────
    onScroll() {
      const log = logEl();
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
        if (gone) return;
        if (isStaleCursor(err)) await this.load();
        else this.error = describeError(err).message;
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
      this.repaint(() => this.scrollToEnd());
    },

    /** Retry after a failure: the first page again, or the catch-up. */
    retry() {
      this.error = "";
      if (this.state === "ready") return this.catchUp();
      this.state = "loading";
      return this.load();
    },

    scrollToEnd() {
      const log = logEl();
      if (log) log.scrollTop = log.scrollHeight;
    },

    /** The first line the reader can see, and how far below the log's top it is. */
    anchor() {
      const log = logEl();
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
      if (!kept) return this.repaint();
      this.repaint(() => {
        const log = logEl();
        const el = log?.querySelector(`[data-cursor="${CSS.escape(kept.cursor)}"]`);
        if (!el) return;
        log.scrollTop += el.getBoundingClientRect().top - log.getBoundingClientRect().top - kept.offset;
      });
    },

    /** After the next render: search matches get their highlight, then then(). */
    repaint(then) {
      this.$nextTick(() => {
        const log = logEl();
        if (typeof Node !== "undefined" && log instanceof Node) highlight(log, this.query);
        then?.();
      });
    },
  };
};

/** localStorage, or null where it is not allowed. */
function safeStorage() {
  try {
    return globalThis.localStorage ?? null;
  } catch {
    return null;
  }
}

/**
 * Each code block in rendered Markdown gets a head with its language and a
 * Copy button, once: the server's html never changes, so neither does it.
 */
function decorateCode(root) {
  for (const pre of root.querySelectorAll("pre:not([data-decorated], .in, .out)")) {
    pre.dataset.decorated = "";
    const lang = /language-([\w+-]+)/.exec(pre.querySelector("code")?.className ?? "")?.[1] ?? "text";
    const block = document.createElement("div");
    block.className = "codeblock";
    const head = document.createElement("div");
    head.className = "head";
    const name = document.createElement("span");
    name.textContent = lang;
    const button = document.createElement("button");
    button.type = "button";
    button.className = "button";
    button.dataset.variant = "ghost";
    button.dataset.size = "sm";
    button.dataset.copyCode = "";
    button.textContent = "Copy";
    head.append(name, button);
    pre.replaceWith(block);
    block.append(head, pre);
  }
}

/** Marks every match of query in the log's text with the CSS highlight. */
function highlight(log, query) {
  const registry = globalThis.CSS?.highlights;
  if (!registry || typeof Highlight === "undefined") return;
  const q = query.trim().toLowerCase();
  if (q === "") {
    registry.delete(HIGHLIGHT);
    return;
  }
  const ranges = [];
  const walk = document.createTreeWalker(log, NodeFilter.SHOW_TEXT);
  for (let node = walk.nextNode(); node; node = walk.nextNode()) {
    if (!node.parentElement?.closest("[data-searchable]")) continue;
    const text = node.data.toLowerCase();
    for (let i = text.indexOf(q); i !== -1; i = text.indexOf(q, i + q.length)) {
      const range = new Range();
      range.setStart(node, i);
      range.setEnd(node, i + q.length);
      ranges.push(range);
    }
  }
  registry.set(HIGHLIGHT, new Highlight(...ranges));
}
