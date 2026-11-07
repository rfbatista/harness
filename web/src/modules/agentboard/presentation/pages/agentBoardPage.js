// The Agent board (the home page): every task with a live session, across
// projects, grouped by project, kept current by the live session feed
// (ADR 0068). It paints from the seed the server rendered into the page; a
// page swapped in place has none, and reads it behind a skeleton that shows
// only when the read is slower than 400 ms.
//
// Rules the page keeps (the study's section 6 and 7):
//  - a card's place changes only when it arrives, starts needing the person,
//    is removed, and when a seed is read (domain/board.js);
//  - a card whose last session ended is held about 6 s and never removed while
//    the pointer is on it or focus is in it;
//  - events at or below the seed's seq are discarded; a gap, a "resynced"
//    stream or a seq that went back make the page read the seed again, once,
//    and reconcile it by task;
//  - needs_you is the server's; the page counts it and never derives it;
//  - polite, coalesced announcements; nothing animates or is announced while
//    the tab is hidden; reduced motion keeps the highlights static.
//
//   <main x-data="agentBoardPage" data-seed="agent-board-seed">

import { FeedStatus } from "../../../../shared/domain/feed.js";
import { skeletonRule } from "../../../../shared/domain/skeletonRule.js";
import { readSeed } from "../../../../shared/presentation/seed.js";
import { applyEvent, emptyBoard, findCard, fromSeed, projectName, reconcile, removeCard } from "../../domain/board.js";
import { announcement, clockTime, needsWord, quietLine, sectionsView, summaryView } from "../view.js";

/** A leaving card stays this long, so the person sees how it ended. */
export const HOLD_MS = 6_000;
/** Its fade and collapse. */
export const EXIT_MS = 260;
/** Announcements of a burst go out as one sentence after this. */
export const ANNOUNCE_MS = 1_500;
/** How long an arrival's ring and a status change's wash last. */
export const FRESH_MS = 2_400;
export const LINE_FRESH_MS = 1_700;
/** A feed that stays down this long gets the reconnecting banner, then the offline one. */
export const RECONNECTING_AFTER_MS = 2_000;
export const OFFLINE_AFTER_MS = 10_000;
/** The skeleton waits this long (the study's latency budget). */
export const SKELETON_AFTER_MS = 400;
/** Times on the lines ("4m") are refreshed this often. */
export const CLOCK_MS = 30_000;

const BASE_TITLE = /^\(\d+\+?\) /;

/**
 * @param {{
 *   gateway: import("../../domain/ports.js").AgentBoardGateway,
 *   navigate: (url: string) => void,
 *   clock: import("../../../../shared/infrastructure/clock.js").Clock,
 *   timers?: { setTimeout: typeof setTimeout, clearTimeout: typeof clearTimeout },
 *   isVisible?: () => boolean,
 *   reducedMotion?: () => boolean,
 *   doc?: { title: string },
 * }} deps
 */
export const agentBoardPage =
  ({
    gateway,
    navigate,
    clock,
    timers = globalThis,
    isVisible = () => !globalThis.document?.hidden,
    reducedMotion = () => globalThis.matchMedia?.("(prefers-reduced-motion: reduce)")?.matches === true,
    doc = globalThis.document,
  }) =>
  () => {
    // Not reactive: timers, the single-flight read, the events waiting for it.
    const timer = { holds: new Map(), pending: new Set(), clear: new Map() };
    let announceQueue = [];
    let announceTimer = null;
    let bannerTimers = [];
    let clockTimer = null;
    let reading = null;
    let waiting = [];
    let stopFeed = () => {};
    let baseTitle = "";
    let root = null;
    let skeleton = null;
    let destroyed = false;

    const after = (ms, fn) => timers.setTimeout(fn, ms);

    return {
      /** The board as the domain describes it (domain/board.js). */
      board: emptyBoard(),
      query: "",
      now: clock.now(),
      loaded: false,
      ready: false,
      /** The seed could not be read and nothing is on screen. */
      failed: false,
      /** "reconnecting", "offline" or null: the feed is down and how long. */
      down: null,
      lastUpdate: clock.now(),
      liveText: "",
      cursorId: "",
      hoverId: "",
      focusId: "",
      expanded: [],
      freshCards: [],
      freshLines: [],
      arriving: [],
      exiting: [],
      skeletonShown: false,
      skeletonIn: "",

      get sections() {
        return sectionsView(this.board, this.now, this.ui);
      },
      get ui() {
        return {
          query: this.query,
          expanded: this.expanded,
          freshCards: this.freshCards,
          freshLines: this.freshLines,
          arriving: this.arriving,
          exiting: this.exiting,
        };
      },
      get summary() {
        return summaryView(this.board);
      },
      get quiet() {
        return this.ready && !this.failed ? quietLine(this.board.quietProjects) : "";
      },
      get cards() {
        return this.sections.flatMap((s) => s.cards).filter((c) => !c.leaving);
      },
      get isEmpty() {
        return this.board.sections.length === 0;
      },
      get showBoard() {
        return this.ready && !this.failed && !this.isEmpty && this.sections.length > 0;
      },
      get showEmpty() {
        return this.ready && !this.failed && this.isEmpty;
      },
      get noMatch() {
        return this.ready && !this.failed && !this.isEmpty && this.sections.length === 0;
      },
      get noMatchText() {
        return `No task, project or agent matches “${this.query.trim()}”.`;
      },
      /** The tasks that need the person, as the server said. */
      get needsCount() {
        return this.summary.needsCount;
      },
      get banner() {
        if (this.failed) {
          return {
            tone: "danger",
            role: "alert",
            lead: "Could not load the agent board.",
            text: "The harness did not answer. Your agents are not affected.",
            code: "BOARD_SEED_FAILED",
            action: "Try again",
            projects: true,
          };
        }
        const at = clockTime(this.lastUpdate);
        if (this.down === "offline") {
          return {
            tone: "danger",
            role: "alert",
            lead: "The harness is not reachable.",
            text: `The board stopped updating at ${at}. It will refresh by itself when the connection returns.`,
            code: "",
            action: "Retry now",
            projects: false,
          };
        }
        if (this.down === "reconnecting") {
          return {
            tone: "attention",
            role: "status",
            lead: "Reconnecting.",
            text: `What you see is as of ${at} and may be out of date.`,
            code: "",
            action: "Retry now",
            projects: false,
          };
        }
        return null;
      },

      init() {
        root = this.$el;
        baseTitle = (doc?.title ?? "").replace(BASE_TITLE, "");
        skeleton = skeletonRule({
          timers: { setTimeout: timers.setTimeout.bind(timers), clearTimeout: timers.clearTimeout.bind(timers), now: () => clock.now().getTime() },
          showAfter: SKELETON_AFTER_MS,
          onChange: ({ visible, fade }) => {
            this.skeletonShown = visible;
            if (fade) this.skeletonIn = "skeleton-in";
          },
        });
        // Follow first, so an event that comes while the seed is read is not lost.
        stopFeed = gateway.follow(
          (event) => this.onEvent(event),
          (status) => this.onStatus(status),
        );
        clockTimer = after(CLOCK_MS, () => this.tickClock());
        let seed = null;
        if (root.dataset.seed) {
          try {
            seed = gateway.decodeSeed(readSeed(root));
          } catch {
            seed = null; // the server's seed is unusable: read it like a swapped page
          }
        }
        if (seed) {
          this.setBoard(fromSeed(seed));
          this.$nextTick(() => {
            for (const node of root.querySelectorAll("[data-ssr]")) node.remove();
            this.ready = true;
          });
        } else {
          this.load();
        }
      },

      destroy() {
        destroyed = true;
        stopFeed();
        skeleton?.stop();
        timers.clearTimeout(clockTimer);
        timers.clearTimeout(announceTimer);
        for (const t of bannerTimers) timers.clearTimeout(t);
        for (const t of timer.holds.values()) timers.clearTimeout(t);
        for (const t of timer.clear.values()) timers.clearTimeout(t);
        timer.holds.clear();
        timer.clear.clear();
      },

      /** The first read, for a page that came without a seed. */
      async load() {
        this.failed = false;
        skeleton.begin();
        try {
          const seed = await gateway.readSeed();
          await skeleton.end();
          if (destroyed) return;
          this.setBoard(fromSeed(seed));
          this.ready = true;
          this.drain();
        } catch {
          skeleton.fail();
          if (destroyed) return;
          this.failed = true;
        }
      },

      setBoard(board) {
        this.board = board;
        this.loaded = true;
        this.touch();
      },

      /** A change reached the board: the time it is "as of", and the tab title. */
      touch() {
        this.lastUpdate = clock.now();
        this.now = this.lastUpdate;
        if (doc) doc.title = this.needsCount ? `(${this.needsCount}) ${baseTitle}` : baseTitle;
      },

      tickClock() {
        this.now = clock.now();
        clockTimer = after(CLOCK_MS, () => this.tickClock());
      },

      onVisibility() {
        if (isVisible()) this.now = clock.now();
      },

      // ---- the feed ----

      onEvent(event) {
        if (!this.loaded || reading) {
          waiting.push(event);
          return;
        }
        const result = applyEvent(this.board, event);
        if (result.outcome === "stale") return;
        if (result.outcome === "applied") {
          this.commit(result.board, result.notes);
          return;
        }
        waiting = [event]; // a gap, or an event that needs its project's name: the seed answers both
        this.reseed();
      },

      /** The events that came while the seed was read, in order, after it. */
      drain() {
        const events = waiting.splice(0).sort((a, b) => a.seq - b.seq);
        for (const event of events) this.onEvent(event);
      },

      onStatus(status) {
        this.$dispatch("feed-status", status);
        if (status === FeedStatus.PAUSED) {
          if (bannerTimers.length === 0) {
            bannerTimers = [
              after(RECONNECTING_AFTER_MS, () => (this.down = "reconnecting")),
              after(OFFLINE_AFTER_MS, () => (this.down = "offline")),
            ];
          }
          return;
        }
        for (const t of bannerTimers) timers.clearTimeout(t);
        bannerTimers = [];
        this.down = null;
        if (status === FeedStatus.RESYNCED) this.reseed();
      },

      /** Reads the seed again (one read at a time) and reconciles it with the board. */
      reseed() {
        if (reading) return reading;
        reading = (async () => {
          try {
            const seed = await gateway.readSeed();
            if (destroyed) return;
            const wholesale = seed.seq < this.board.seq;
            const { board, notes } = reconcile(this.board, seed, { wholesale });
            this.commit(board, notes);
          } catch {
            // the cards stay as they are; the stream bar and the banner tell the connection
          } finally {
            reading = null;
          }
          if (!destroyed) this.drain();
        })();
        return reading;
      },

      /** The banner's and the error's button. */
      retry() {
        if (!this.loaded) return this.load();
        return this.reseed();
      },

      // ---- change ----

      /** Takes the next board and plays what changed: the highlights, the glide, the announcements, the holds. */
      commit(next, notes) {
        const visible = isVisible();
        const before = visible && !reducedMotion() ? this.measure() : null;
        this.board = next;
        for (const note of notes) this.note(note, visible);
        this.touch();
        if (before) this.$nextTick(() => this.glide(before));
      },

      note(note, visible) {
        const card = findCard(this.board, note.taskId);
        const title = card?.taskTitle ?? "";
        const project = projectName(this.board, card?.projectId ?? "");
        switch (note.type) {
          case "arrived":
            if (!visible) return;
            this.arriving = [...this.arriving, note.taskId];
            this.markFresh("freshCards", note.taskId, FRESH_MS);
            this.announce(`${project}: ${title} started`);
            break;
          case "needs":
            if (!visible) return;
            this.announce(`${project}: ${title} needs you, ${needsWord(card)}`);
            break;
          case "line":
            if (visible) this.markFresh("freshLines", `${note.taskId}/${note.sessionId}`, LINE_FRESH_MS);
            break;
          case "leaving":
            this.startHold(note.taskId);
            break;
          case "revived":
            this.cancelHold(note.taskId);
            break;
        }
      },

      /** Puts id in a highlight list for ms. */
      markFresh(list, id, ms) {
        this[list] = [...this[list].filter((x) => x !== id), id];
        const key = `${list}/${id}`;
        timers.clearTimeout(timer.clear.get(key));
        timer.clear.set(
          key,
          after(ms, () => {
            this[list] = this[list].filter((x) => x !== id);
            if (list === "freshCards") this.arriving = this.arriving.filter((x) => x !== id);
            timer.clear.delete(key);
          }),
        );
      },

      // ---- a card on its way out ----

      startHold(taskId) {
        this.cancelHold(taskId);
        timer.holds.set(taskId, after(HOLD_MS, () => this.release(taskId)));
      },

      cancelHold(taskId) {
        timers.clearTimeout(timer.holds.get(taskId));
        timer.holds.delete(taskId);
        timer.pending.delete(taskId);
        this.exiting = this.exiting.filter((x) => x !== taskId);
      },

      /** The hold is over: the card goes, unless the pointer or focus is on it; then it goes when they leave. */
      release(taskId) {
        timer.holds.delete(taskId);
        if (taskId === this.hoverId || taskId === this.focusId) {
          timer.pending.add(taskId);
          return;
        }
        timer.pending.delete(taskId);
        this.exiting = [...this.exiting, taskId];
        timer.holds.set(
          taskId,
          after(reducedMotion() ? 90 : EXIT_MS, () => {
            timer.holds.delete(taskId);
            this.exiting = this.exiting.filter((x) => x !== taskId);
            const before = isVisible() && !reducedMotion() ? this.measure() : null;
            this.board = removeCard(this.board, taskId);
            this.expanded = this.expanded.filter((x) => x !== taskId);
            this.touch();
            if (before) this.$nextTick(() => this.glide(before));
          }),
        );
      },

      hover(id) {
        this.hoverId = id;
      },
      unhover(id) {
        if (this.hoverId === id) this.hoverId = "";
        this.releaseIfDue(id);
      },
      focused(id) {
        this.focusId = this.cursorId = id;
      },
      /** @param {string} id @param {FocusEvent} [event] focus moving inside the card is not leaving it */
      blurred(id, event) {
        if (event?.currentTarget?.contains?.(event.relatedTarget)) return;
        if (this.focusId === id) this.focusId = "";
        this.releaseIfDue(id);
      },
      releaseIfDue(id) {
        if (timer.pending.has(id) && this.hoverId !== id && this.focusId !== id) this.release(id);
      },

      // ---- motion ----

      /** Where the cards are now, by task. */
      measure() {
        const rects = new Map();
        for (const el of root?.querySelectorAll?.(".agent-card[data-task]") ?? []) rects.set(el.dataset.task, el.getBoundingClientRect());
        return rects;
      },

      /** The cards a change pushed aside glide to their new place (FLIP); the rest stay still. */
      glide(before) {
        for (const el of root?.querySelectorAll?.(".agent-card[data-task]") ?? []) {
          const was = before.get(el.dataset.task);
          if (!was || !el.animate) continue;
          const now = el.getBoundingClientRect();
          const dx = was.left - now.left;
          const dy = was.top - now.top;
          if (dx || dy) el.animate([{ transform: `translate(${dx}px, ${dy}px)` }, { transform: "none" }], { duration: 240, easing: "cubic-bezier(0.25, 1, 0.5, 1)" });
        }
      },

      // ---- announcements ----

      /** One polite sentence per burst. */
      announce(message) {
        announceQueue.push(message);
        timers.clearTimeout(announceTimer);
        announceTimer = after(ANNOUNCE_MS, () => {
          this.liveText = announcement(announceQueue);
          announceQueue = [];
        });
      },

      // ---- the person ----

      toggleMore(id) {
        this.expanded = this.expanded.includes(id) ? this.expanded.filter((x) => x !== id) : [...this.expanded, id];
      },

      /** Moves keyboard focus by delta among the cards shown, across sections. */
      move(delta) {
        const cards = this.cards;
        if (cards.length === 0) return;
        const at = cards.findIndex((c) => c.id === this.cursorId);
        const next = cards[Math.max(0, Math.min(cards.length - 1, at < 0 ? (delta > 0 ? 0 : cards.length - 1) : at + delta))];
        this.cursorId = next.id;
        const link = root?.querySelector?.(`[data-task="${globalThis.CSS?.escape?.(next.id) ?? next.id}"] .title a`);
        link?.focus?.();
        link?.scrollIntoView?.({ block: "nearest" });
      },

      /** The page's keys; typing in a field is left alone. @param {KeyboardEvent} event */
      key(event) {
        if (event.metaKey || event.ctrlKey || event.altKey || event.defaultPrevented) return;
        const tag = event.target?.tagName;
        if (tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT") return;
        // A focused link or button already answers Enter itself.
        const onControl = tag === "A" || tag === "BUTTON";
        const card = this.cards.find((c) => c.id === this.cursorId);
        const actions = {
          j: () => this.move(1),
          k: () => this.move(-1),
          Enter: () => !onControl && card && navigate(card.href),
          "/": () => this.$refs.filter?.focus(),
        };
        const action = actions[event.key];
        if (!action || (event.key === "Enter" && (onControl || !card))) return;
        event.preventDefault();
        action();
      },

      /** Esc in the filter clears it and hands the keys back to the cards. */
      clearFilter() {
        this.query = "";
        this.$refs.filter?.blur();
      },
    };
  };
