// The project's Usage page: totals by token kind with the estimated cost,
// tokens over time by model, the top tasks and sessions, and a live feed of
// requests as they finish. Server-rendered with a seed (today's summary and
// the recent records); each record on the project's usage stream joins the
// feed and the current range's numbers (domain/usage.js applyRecord).
//
//   <main x-data="usagePage" data-seed="usage-seed">

import { FeedStatus } from "../../../../shared/domain/feed.js";
import { describeError } from "../../../../shared/presentation/errors.js";
import { readSeed } from "../../../../shared/presentation/seed.js";
import { applyRecord, RANGES } from "../../domain/usage.js";
import { chart, feedRow, modelSlots, sessionRows, taskRows, totalTiles } from "../view.js";

const RANGE_LABEL = { today: "Today", "7d": "7 days", "30d": "30 days" };
const SVG = "http://www.w3.org/2000/svg";
/** The chart's drawing box: one 10-unit column per bucket, 192 units (px) tall. */
const COLUMN = 10;
const BAR = 7;
const HEIGHT = 192;
/** The surface gap between stacked segments. */
const GAP = 2;

/**
 * @param {{ gateway: import("../../domain/ports.js").UsageGateway }} deps
 */
export const usagePage = ({ gateway }) => () => {
  let unfollow = () => {};
  /** The summary read in flight: a newer range aborts it. */
  let reading = null;

  return {
    projectId: "",
    /** @type {import("../../domain/usage.js").Range} */
    range: "today",
    /** @type {import("../../domain/usage.js").UsageSummary|null} */
    summary: null,
    /** @type {import("../../domain/usage.js").UsageRecord[]} */
    records: [],
    /** Each model's colour slot, kept for the page's life. */
    slots: {},
    loading: false,
    live: false,
    error: null,

    // ── what the markup binds ────────────────────────────────────────────
    get tabs() {
      return RANGES.map((id) => ({ id, label: RANGE_LABEL[id], pressed: id === this.range ? "true" : "false" }));
    },
    get loaded() {
      return this.summary !== null;
    },
    get tiles() {
      return this.summary ? totalTiles(this.summary.totals) : [];
    },
    get chartView() {
      return this.summary ? chart(this.summary, this.slots) : { columns: [], max: 0, maxLabel: "0", legend: [], empty: true };
    },
    get ticks() {
      return this.chartView.columns.filter((c) => c.tick);
    },
    get taskRows() {
      return this.summary ? taskRows(this.summary.topTasks, this.projectId, this.summary.totals.total) : [];
    },
    get sessionRows() {
      return this.summary ? sessionRows(this.summary.topSessions, this.projectId, this.summary.totals.total) : [];
    },
    get noTasks() {
      return this.loaded && this.taskRows.length === 0;
    },
    get noSessions() {
      return this.loaded && this.sessionRows.length === 0;
    },
    get feedRows() {
      return this.records.map((r) => feedRow(r, this.projectId));
    },
    get feedEmpty() {
      return this.records.length === 0;
    },
    get liveTone() {
      return this.live ? "success" : "attention";
    },
    get liveText() {
      return this.live ? "Live" : "Reconnecting…";
    },
    get rangeLabel() {
      return RANGE_LABEL[this.range];
    },

    // ── lifecycle ────────────────────────────────────────────────────────
    init() {
      try {
        const seed = gateway.decodeSeed(readSeed(this.$el));
        this.projectId = seed.projectId;
        this.records = seed.records;
        if (seed.summary) this.show(seed.summary);
      } catch (err) {
        this.error = describeError(err);
        return;
      }
      if (!this.summary) this.load();
      unfollow = gateway.follow(
        this.projectId,
        (record) => this.arrived(record),
        (status) => this.status(status),
      );
    },

    destroy() {
      unfollow();
      reading?.abort();
    },

    status(status) {
      this.live = status === FeedStatus.LIVE;
      if (status === FeedStatus.RESYNCED) this.resync();
    },

    /** A request finished: it joins the feed and the current range's numbers. */
    arrived(record) {
      const next = applyRecord({ summary: this.summary, records: this.records }, record);
      if (next.summary !== this.summary && next.summary) this.slots = modelSlots(this.slots, next.summary.series);
      this.summary = next.summary;
      this.records = next.records;
    },

    show(summary) {
      this.slots = modelSlots(this.slots, summary.series);
      this.summary = summary;
      this.range = summary.range;
    },

    /** A range tab: reads its summary; the feed stays as it is. */
    pick(event) {
      const range = event.currentTarget?.dataset.range ?? event.target?.closest?.("[data-range]")?.dataset.range;
      if (!RANGES.includes(range) || range === this.range) return;
      this.range = range;
      this.load();
    },

    async load() {
      reading?.abort();
      const mine = new AbortController();
      reading = mine;
      this.loading = true;
      try {
        const summary = await gateway.summary(this.projectId, this.range, mine.signal);
        if (reading !== mine) return;
        this.show(summary);
        this.error = null;
      } catch (err) {
        if (err?.name === "AbortError" || reading !== mine) return;
        this.error = describeError(err);
      } finally {
        if (reading === mine) {
          this.loading = false;
          reading = null;
        }
      }
    },

    /** Records were missed while the stream was down: read both again. */
    async resync() {
      this.load();
      try {
        this.records = await gateway.recent(this.projectId);
      } catch (err) {
        this.error = describeError(err);
      }
    },

    dismissError() {
      this.error = null;
    },

    /**
     * Draws the tokens-over-time chart into the <svg> the markup gives it
     * (x-effect, so it redraws as the numbers change): a stacked bar per
     * bucket, a 2px gap between segments, a hover title per column.
     * @param {SVGSVGElement} svg
     */
    draw(svg) {
      const view = this.chartView;
      const n = view.columns.length;
      svg.setAttribute("viewBox", `0 0 ${Math.max(1, n * COLUMN)} ${HEIGHT}`);
      svg.replaceChildren();
      const scale = view.max > 0 ? (HEIGHT - GAP) / view.max : 0;
      const el = (name, attrs) => {
        const node = document.createElementNS(SVG, name);
        for (const [k, v] of Object.entries(attrs)) node.setAttribute(k, String(v));
        return node;
      };
      view.columns.forEach((c, i) => {
        const g = el("g", { class: "column" });
        const title = el("title", {});
        title.textContent = c.title;
        g.append(title, el("rect", { class: "hit", x: i * COLUMN, y: 0, width: COLUMN, height: HEIGHT }));
        // Bottom up; each segment above another gives up GAP to the surface.
        let y = HEIGHT;
        for (const [k, s] of c.segments.entries()) {
          const h = s.total * scale;
          y -= h;
          const height = Math.max(1, k === 0 ? h : h - GAP);
          g.append(el("rect", { "data-slot": s.slot, x: i * COLUMN + (COLUMN - BAR) / 2, y, width: BAR, height }));
        }
        svg.append(g);
      });
      svg.append(el("line", { class: "baseline", x1: 0, x2: n * COLUMN, y1: HEIGHT - 0.5, y2: HEIGHT - 0.5 }));
    },
  };
};
