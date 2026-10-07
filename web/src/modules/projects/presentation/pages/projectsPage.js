// Every project at a glance: whether agents work there now, its
// repositories, open tasks and last activity. Projects created, renamed or
// deleted anywhere show up as it happens (the catalog feed); counts are not
// on that feed, so the list asks for fresh ones shortly after any change,
// when the feed comes back, when the tab is shown again, and every 30 s.
// Keyboard: j/k or arrows move, Enter/l open, s settings, n new project,
// / filter, r refresh — the tui-client's project list keys where they meet.
//
//   <main x-data="projectsListPage" data-seed="projects-seed">

import { FeedStatus } from "../../../../shared/domain/feed.js";
import { describeError } from "../../../../shared/presentation/errors.js";
import { readSeed } from "../../../../shared/presentation/seed.js";
import { byName, matchesQuery, sessionsRunning } from "../../domain/project.js";
import { summaryView, totalLine } from "../view.js";

/** After a catalog change, wait this long for more before asking for counts. */
export const SETTLE_MS = 1_000;
/** Counts change without a catalog event (a task moves, a session ends). */
export const REFRESH_MS = 30_000;

const projectHref = (id) => `/projects/${encodeURIComponent(id)}`;

/**
 * @param {{
 *   gateway: import("../../domain/ports.js").ProjectGateway,
 *   navigate: (url: string) => void,
 *   clock: import("../../../../shared/infrastructure/clock.js").Clock,
 *   timers?: { setTimeout: typeof setTimeout, clearTimeout: typeof clearTimeout },
 *   isVisible?: () => boolean,
 * }} deps
 */
export const projectsPage =
  ({ gateway, navigate, clock, timers = globalThis, isVisible = () => !globalThis.document?.hidden }) =>
  () => ({
    /** @type {import("../../domain/project.js").ProjectSummary[]} */
    summaries: [],
    query: "",
    selectedId: "",
    /** Projects that just arrived over the feed: a wash that fades. */
    freshIds: [],
    now: clock.now(),
    error: null,
    ready: false,
    timer: null,
    stopFeed: () => {},

    get rows() {
      return this.summaries
        .filter((s) => matchesQuery(s, this.query))
        .map((s) => ({
          id: s.project.id,
          name: s.project.name,
          rootDir: s.project.rootDir,
          ...summaryView(s, this.now),
          href: projectHref(s.project.id),
          settingsHref: `${projectHref(s.project.id)}/settings`,
          settingsLabel: `Settings of ${s.project.name}`,
          selected: s.project.id === this.selectedId,
          fresh: this.freshIds.includes(s.project.id),
        }));
    },
    get total() {
      return totalLine(this.summaries.length, sessionsRunning(this.summaries));
    },
    get isEmpty() {
      return this.summaries.length === 0;
    },
    get showList() {
      return this.ready && !this.isEmpty;
    },
    get showEmpty() {
      return this.ready && this.isEmpty;
    },
    get noMatch() {
      return !this.isEmpty && this.rows.length === 0;
    },

    init() {
      try {
        this.summaries = gateway.decodeProjectsSeed(readSeed(this.$el));
      } catch (err) {
        this.error = describeError(err);
        return;
      }
      this.selectedId = this.summaries[0]?.project.id ?? "";
      this.$nextTick(() => {
        for (const node of this.$el.querySelectorAll("[data-ssr]")) node.remove();
        this.ready = true;
      });
      this.stopFeed = gateway.followCatalog(
        (change) => this.apply(change),
        (status) => {
          this.$dispatch("feed-status", status);
          if (status === FeedStatus.RESYNCED) this.schedule(0);
        },
      );
      this.schedule(REFRESH_MS);
    },

    destroy() {
      this.stopFeed();
      timers.clearTimeout(this.timer);
    },

    /** @param {import("../../domain/ports.js").CatalogChange} change */
    apply(change) {
      if (change.kind === "deleted") {
        const at = this.rows.findIndex((r) => r.id === change.id);
        this.summaries = this.summaries.filter((s) => s.project.id !== change.id);
        if (this.selectedId === change.id) this.selectedId = this.rows[Math.min(at, this.rows.length - 1)]?.id ?? "";
      } else {
        const { project } = change;
        const known = this.summaries.find((s) => s.project.id === project.id);
        const next = known
          ? { ...known, project }
          : { project, repositoryCount: 0, openTaskCount: 0, runningSessionCount: 0, lastActivityAt: null };
        this.summaries = [...this.summaries.filter((s) => s.project.id !== project.id), next].sort(byName);
        if (!known) this.freshIds = [...this.freshIds, project.id];
        if (!this.selectedId) this.selectedId = project.id;
      }
      this.schedule(SETTLE_MS);
    },

    /** Asks for fresh counts after ms, replacing any earlier plan. */
    schedule(ms) {
      timers.clearTimeout(this.timer);
      this.timer = timers.setTimeout(() => this.refresh(), ms);
    },

    /** Fresh counts; a failure keeps what is shown (the stream bar tells the connection). */
    async refresh() {
      if (isVisible()) {
        try {
          this.summaries = await gateway.listProjectSummaries();
          this.now = clock.now();
          if (!this.rows.some((r) => r.id === this.selectedId)) this.selectedId = this.rows[0]?.id ?? "";
        } catch {
          // stale counts until the next try
        }
      }
      this.schedule(REFRESH_MS);
    },

    onVisibility() {
      if (isVisible()) this.schedule(0);
    },

    /** Moves the keyboard cursor by delta among the rows shown. */
    move(delta) {
      const rows = this.rows;
      if (rows.length === 0) return;
      const at = rows.findIndex((r) => r.id === this.selectedId);
      const next = rows[Math.max(0, Math.min(rows.length - 1, at < 0 ? 0 : at + delta))];
      this.selectedId = next.id;
      this.$nextTick(() => this.$el.querySelector(`[data-project-id="${CSS.escape(next.id)}"]`)?.scrollIntoView({ block: "nearest" }));
    },

    select(id) {
      this.selectedId = id;
    },

    /** The page's keys; typing in a field is left alone. @param {KeyboardEvent} event */
    key(event) {
      if (event.metaKey || event.ctrlKey || event.altKey || event.defaultPrevented) return;
      const tag = event.target?.tagName;
      if (tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT") return;
      const selected = this.rows.find((r) => r.id === this.selectedId);
      // A focused link or button already answers Enter itself.
      const onControl = tag === "A" || tag === "BUTTON";
      const actions = {
        j: () => this.move(1),
        ArrowDown: () => this.move(1),
        k: () => this.move(-1),
        ArrowUp: () => this.move(-1),
        Enter: () => !onControl && selected && navigate(selected.href),
        l: () => selected && navigate(selected.href),
        ArrowRight: () => selected && navigate(selected.href),
        s: () => selected && navigate(selected.settingsHref),
        n: () => navigate("/projects/new"),
        "/": () => this.$refs.filter?.focus(),
        r: () => this.schedule(0),
      };
      const action = actions[event.key];
      if (!action || (event.key === "Enter" && onControl)) return;
      event.preventDefault();
      action();
    },

    /** Esc in the filter clears it and hands the keys back to the list. */
    clearFilter() {
      this.query = "";
      this.$refs.filter?.blur();
    },

    /** The filter keeps the cursor on a row it still shows. */
    filtered() {
      if (!this.rows.some((r) => r.id === this.selectedId)) this.selectedId = this.rows[0]?.id ?? "";
    },

    dismissError() {
      this.error = null;
    },
  });
