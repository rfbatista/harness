// A repository's Architecture page: one draft of the whole record, edited by
// the tabs, beside the saved version it came from.
//
//   <main x-data="architecturePage" data-seed="architecture-seed">
//
// The tabs are deep links the page switches between without a reload.

import { describeError } from "../../../../shared/presentation/errors.js";
import { readSeed } from "../../../../shared/presentation/seed.js";
import {
  ARCHITECTURE_CHANGED,
  ARCHITECTURE_INVALID,
  TABS,
  cleanRecord,
  cloneRecord,
  describePointer,
  diffRecords,
  exportBadge,
  pathForTab,
  sameRecord,
  statCards,
  tabCounts,
  changeSummary,
  tabForPath,
  tabOfPointer,
} from "../../domain/record.js";

import { EVENTS_COMPACT, prependEvent, ruleEventRow, shownEvents } from "../../domain/events.js";
import { hexagonalLayers } from "../../domain/layers.js";

/** @param {{ gateway: ReturnType<typeof import("../../infrastructure/architecture-gateway.js").architectureGateway> }} deps */
export const architecturePage = ({ gateway }) => () => ({
  projectId: "",
  repositoryId: "",
  /** The repository's checkout and the paths it hides from trees, for the folder picker. */
  rootDir: "",
  ignoredPaths: [],
  base: "",
  /** The open tab's slug, and for Output its view (file or diagram). */
  tab: TABS[0].slug,
  view: "",
  loaded: false,
  error: null,
  /** The saved version: record, version number, whether it exists, the export state. */
  saved: null,
  /** The person's working copy of the record; every tab edits it. */
  draft: null,
  version: 0,
  exists: false,
  exportInfo: null,
  saving: false,
  /** The highest version the feed announced while our own save was in flight (0: none). */
  announced: 0,
  /** What ARCHITECTURE_INVALID last said, one entry per field: { path, code, message }. */
  errors: [],
  /** Why the draft is out of date: "" when it is not, "save" (a save was refused) or "feed" (a save elsewhere was announced). */
  conflict: "",
  /** The server's newest version, once a conflict made the page fetch it. */
  remote: null,
  diffRows: [],
  showDiff: false,
  unfollow: null,
  /** Recent rule events of the repository, newest first, as the API names them. */
  events: [],
  /** Whether the events panel shows every event, not only the first few. */
  allEvents: false,
  /** How many paths match more than one layer: the Layers tab sets it from its preview. */
  overlapCount: 0,
  /** Whether the page was opened on a tab's deep link and has not yet scrolled to the tabs. */
  deepLinked: false,

  get eventRows() {
    const now = new Date();
    return shownEvents(this.events, this.allEvents).map((e) => ruleEventRow(e, this.projectId, now));
  },
  get hasEvents() {
    return this.events.length > 0;
  },
  get hasMoreEvents() {
    return this.events.length > EVENTS_COMPACT;
  },
  get eventsToggleText() {
    return this.allEvents ? "Show fewer" : `Show all (${this.events.length})`;
  },

  get isNew() {
    return this.loaded && !this.exists;
  },
  get stats() {
    return this.draft ? statCards(this.draft, this.overlapCount) : [];
  },
  /** Whether the draft has no layers yet, so the hexagonal preset has nothing to replace. */
  get canStartHexagonal() {
    return !!this.draft && (this.draft.layers ?? []).length === 0;
  },
  get exportBadge() {
    return exportBadge(this.exportInfo);
  },
  get dirty() {
    return !!this.draft && !!this.saved && !sameRecord(this.draft, this.saved.record);
  },
  get changeText() {
    return this.dirty ? changeSummary(diffRecords(this.saved.record, this.draft)) : "0 changes";
  },
  get barSummary() {
    const n = this.errors.length;
    return n === 0 ? this.changeText : `${this.changeText} · ${n} ${n === 1 ? "problem" : "problems"}`;
  },
  get problems() {
    return this.errors.map((e) => {
      const tab = TABS.find((t) => t.slug === tabOfPointer(e.path));
      return { path: e.path, label: describePointer(e.path), message: e.message, slug: tab.slug, tabLabel: tab.label };
    });
  },
  get hasProblems() {
    return this.errors.length > 0;
  },
  get problemsText() {
    const n = this.errors.length;
    return `${n} ${n === 1 ? "problem stops" : "problems stop"} this from saving. Fix the marked fields.`;
  },
  get saveBarVisible() {
    return this.dirty || this.conflict !== "";
  },
  get conflictText() {
    return this.conflict === "feed"
      ? "This architecture was saved elsewhere. Your draft is kept."
      : "This architecture was saved by someone else since you opened it. Your draft is kept and was not saved.";
  },
  get diffTitle() {
    return this.remote ? "Your draft against the newest saved version" : "Your draft against the saved version";
  },
  get hasDiff() {
    return this.diffRows.length > 0;
  },
  get tabs() {
    const counts = tabCounts(this.draft);
    return TABS.map((t) => ({
      slug: t.slug,
      label: t.label,
      hasCount: this.draft !== null && counts[t.slug] !== null,
      count: counts[t.slug],
      id: `architecture-tab-${t.slug}`,
      panelId: `architecture-panel-${t.slug}`,
      selected: t.slug === this.tab,
    }));
  },

  isTab(slug) {
    return this.tab === slug;
  },

  init() {
    try {
      const seed = readSeed(this.$el);
      this.projectId = seed.project_id;
      this.repositoryId = seed.repository_id;
      this.rootDir = seed.root_dir ?? "";
      this.ignoredPaths = Array.isArray(seed.ignored_paths) ? seed.ignored_paths : [];
      this.base = seed.base;
      this.deepLinked = (seed.tab ?? "") !== "";
      this.applyPath(seed.tab ?? "");
    } catch (err) {
      this.error = describeError(err);
      return;
    }
    window.addEventListener("popstate", () => this.applyPath(this.pathFromLocation()));
    this.load();
    this.unfollow = gateway.follow(this.projectId, this.repositoryId, {
      onChanged: (change) => this.followed(change),
      onRuleEvent: (ev) => {
        this.events = prependEvent(this.events, ev);
      },
    });
    this.loadEvents();
  },

  destroy() {
    this.unfollow?.();
  },

  /** The overview's recent rule events; a failure leaves the list empty, the page still works. */
  async loadEvents() {
    try {
      this.events = await gateway.listRuleEvents(this.repositoryId);
    } catch {
      this.events = [];
    }
  },

  /** A save, delete or export announced on the project feed. */
  followed(change) {
    if (this.saving) {
      // Probably our own save, but possibly another one that landed after it: keep the highest and judge once the save answers.
      this.announced = Math.max(this.announced, change.version);
      return;
    }
    if (change.version === this.version) {
      this.refreshExport(); // an export announces the version it exported, unchanged
      return;
    }
    if (!this.dirty) {
      this.load();
      return;
    }
    this.conflict = "feed";
  },

  /** Reads the export state again, leaving the draft and the saved version alone. */
  async refreshExport() {
    try {
      const state = await gateway.get(this.repositoryId);
      if (state.version === this.version) this.takeExport(state.export);
    } catch {
      // the feed will say again; the state shown stays as it was
    }
  },

  /** The export state after an export, or a refresh. */
  takeExport(info) {
    this.exportInfo = info;
  },

  pathFromLocation() {
    const rest = window.location.pathname.startsWith(this.base) ? window.location.pathname.slice(this.base.length) : "";
    return rest.replace(/^\//, "");
  },

  applyPath(path) {
    this.tab = tabForPath(path).slug;
    this.view = path === "file" || path === "diagram" ? path : "";
  },

  /** Opens a tab without leaving the page; the address follows it. */
  openTab(slug) {
    if (slug === this.tab) return;
    this.tab = slug;
    this.view = "";
    const path = pathForTab(slug);
    window.history.pushState(null, "", path ? `${this.base}/${path}` : this.base);
  },

  /** Opens one of a tab's views (Output: file or diagram) and puts it in the address. */
  openView(view) {
    if (view === this.view && this.tab === "output") return;
    this.tab = tabForPath(view).slug;
    this.view = view;
    window.history.pushState(null, "", `${this.base}/${pathForTab(this.tab, view)}`);
  },

  async load() {
    try {
      this.take(await gateway.get(this.repositoryId));
      this.error = null;
      this.revealDeepLink();
    } catch (err) {
      this.error = describeError(err);
    }
  },

  /** A deep link opens its tab with the tab list at the top of the viewport. */
  revealDeepLink() {
    if (!this.deepLinked) return;
    this.deepLinked = false;
    this.$nextTick(() => document.getElementById("architecture-tabs")?.scrollIntoView({ block: "start" }));
  },

  toggleEvents() {
    this.allEvents = !this.allEvents;
  },

  /** The empty state's one-click start: the hexagonal layers with the default access. */
  startHexagonal() {
    if (!this.canStartHexagonal) return;
    this.draft.layers = hexagonalLayers();
    if (!this.draft.pattern) this.draft.pattern = "hexagonal";
  },

  errorFor(path) {
    return this.errors.find((e) => e.path === path)?.message ?? "";
  },

  isInvalid(path) {
    return this.errors.some((e) => e.path === path);
  },

  /** Saves the draft as the next version, against the version it started from. */
  async save() {
    if (this.saving || !this.dirty) return;
    this.saving = true;
    this.announced = 0;
    try {
      this.take(await gateway.save(this.repositoryId, this.version, cleanRecord(this.draft)));
      this.clearProblems();
    } catch (err) {
      if (err?.code === ARCHITECTURE_CHANGED) {
        this.conflict = "save";
        this.showDiff = false;
      } else if (err?.code === ARCHITECTURE_INVALID) {
        this.errors = invalidEntries(err.details);
        this.showDiff = false;
      } else {
        this.error = describeError(err);
      }
    } finally {
      this.saving = false;
      const seen = this.announced;
      this.announced = 0;
      // A version beyond the one we hold was saved elsewhere while ours was in flight.
      if (seen > this.version) this.followed({ version: seen });
    }
  },

  /** Throws the draft away for the saved version. */
  discard() {
    this.draft = cloneRecord(this.saved.record);
    this.clearProblems();
  },

  /** Shows what the draft changes against the saved version, or against the newest one after a conflict. */
  async reviewDiff() {
    if (this.showDiff) {
      this.showDiff = false;
      return;
    }
    if (this.conflict) {
      try {
        this.remote = await gateway.get(this.repositoryId);
      } catch (err) {
        this.error = describeError(err);
        return;
      }
    }
    const base = this.remote ?? this.saved;
    this.diffRows = diffRecords(base.record, this.draft);
    this.showDiff = true;
  },

  /** Drops the draft and loads what the server has. */
  async reload() {
    this.clearProblems();
    await this.load();
  },

  clearProblems() {
    this.errors = [];
    this.conflict = "";
    this.remote = null;
    this.showDiff = false;
    this.diffRows = [];
  },

  /** Adopts a state from the server as the saved version and starts a fresh draft from it. */
  take(state) {
    this.saved = state;
    this.draft = cloneRecord(state.record);
    this.version = state.version;
    this.exists = state.exists;
    this.exportInfo = state.export;
    this.loaded = true;
    this.clearProblems();
  },

  dismissError() {
    this.error = null;
  },
});

/** The field errors of an ARCHITECTURE_INVALID refusal: its details are { errors: [...] }. */
function invalidEntries(details) {
  const list = Array.isArray(details?.errors) ? details.errors : [];
  return list
    .filter((e) => typeof e?.path === "string")
    .map((e) => ({ path: e.path, code: e.code ?? "", message: e.message ?? "" }));
}
