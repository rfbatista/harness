// A project's Design system page: one draft of the whole record, edited by the
// tabs, beside the saved version it came from. One Save writes one version.
//
//   <main x-data="designSystemPage" data-seed="design-system-seed">
//
// The tokens are rows in the page's state; the principles and the components
// are held as the server sent them, so a Save never drops what a tab does not
// edit yet. The tabs are addresses (?tab=) the page switches between without
// a reload.

import { FeedStatus } from "../../../../shared/domain/feed.js";
import { describeError } from "../../../../shared/presentation/errors.js";
import { relativeTime } from "../../../../shared/presentation/format.js";
import { readSeed } from "../../../../shared/presentation/seed.js";
import {
  CHANGED,
  INVALID,
  TABS,
  changeText,
  elsewhereText,
  invalidEntries,
  saveBarText,
  sortErrors,
  stateBadge,
  tabForSlug,
} from "../../domain/record.js";
import {
  STANDARD_GROUPS,
  changeCounts,
  filterRows,
  groupCounts,
  newRow,
  placeholderFor,
  recordOf,
  rowStatus,
  rowsFromRecord,
  sampleOf,
} from "../../domain/tokens.js";

/** The fields of a row an error is shown under, by the cell that holds them. */
const CELL_FIELDS = { group: ["group"], name: ["name", "row"], value: ["value", "type"] };

/** Whether the browser reads a CSS value; true where it cannot tell. */
const supports = (property, value) => globalThis.CSS?.supports?.(property, value) ?? true;

/** @param {{ gateway: ReturnType<typeof import("../../infrastructure/designsystem-gateway.js").designSystemGateway>, clock: import("../../../../shared/infrastructure/clock.js").Clock, history?: History, location?: Location }} deps */
export const designSystemPage = ({ gateway, clock, history = globalThis.history, location = globalThis.location }) => () => ({
  projectId: "",
  base: "",
  tab: TABS[0].slug,
  loaded: false,
  error: null,
  /** Whether a version was ever saved, and the one the page holds. */
  exists: false,
  version: 0,
  updatedAt: "",
  updatedBy: "",
  /** Start blank was pressed on a project with no design system: the record is open, nothing is stored. */
  started: false,
  /** The saved record as the server sent it. */
  saved: { tokens: [], principles: "", components: [] },
  /** The draft: the tokens as rows, and the parts no tab edits yet. */
  rows: [],
  principles: "",
  components: [],
  seq: 0,
  filter: "",
  groupFilter: "",
  message: "",
  saving: false,
  /** The highest version the feed announced while our own save was in flight (0: none). */
  announced: 0,
  /** The server's errors for the rows of the last refused save, by row id, and those naming no row. */
  rowErrors: {},
  otherErrors: [],
  /** Why the draft is out of date: "" when it is not, "save" (a save was refused) or "feed" (a save elsewhere was announced). */
  conflict: "",
  /** The newer version, once known (0: not known). */
  elsewhere: 0,
  /** The row being renamed, the token's current name, the names typed, and the server's refusal. */
  renamingId: "",
  renameFrom: "",
  renameGroup: "",
  renameName: "",
  renameError: null,
  unfollow: null,

  get isFirstStart() {
    return this.loaded && !this.exists && !this.started;
  },
  get isOpen() {
    return this.loaded && (this.exists || this.started);
  },
  get counts() {
    return changeCounts(this.rows, this.saved.tokens);
  },
  get principlesChanged() {
    return this.principles !== this.saved.principles;
  },
  get dirty() {
    const c = this.counts;
    return this.isOpen && (c.edited + c.added + c.removed > 0 || this.principlesChanged);
  },
  get canSave() {
    return this.dirty && !this.saving && this.conflict === "";
  },
  get badge() {
    return stateBadge({ exists: this.exists, started: this.started, version: this.version, dirty: this.dirty });
  },
  get barBadge() {
    if (this.dirty) return "Unsaved changes";
    return this.exists ? "Saved" : "Not saved yet";
  },
  get barTone() {
    if (this.dirty) return "attention";
    return this.exists ? "success" : "";
  },
  get changeSummary() {
    return changeText(this.counts, this.principlesChanged);
  },
  get versionText() {
    return saveBarText(this.version);
  },
  get savedLine() {
    const by = this.updatedBy === "person" ? "you" : this.updatedBy;
    const then = new Date(this.updatedAt);
    const ago = Number.isNaN(then.getTime()) ? "" : relativeTime(then, clock.now());
    const when = ago === "" ? "" : ago === "now" ? ", just now" : `, ${ago} ago`;
    return `Version ${this.version} · saved by ${by}${when}`;
  },
  get conflictText() {
    return elsewhereText(this.elsewhere);
  },
  get problems() {
    const out = [];
    for (const row of this.rows) {
      for (const e of this.rowErrors[row.id] ?? []) {
        out.push({ key: `${row.id}:${e.field}:${e.code}`, label: `Tokens › ${row.group || "…"}.${row.name || "…"}`, code: e.code, message: e.message });
      }
    }
    for (const e of this.otherErrors) out.push({ key: `${e.path}:${e.code}`, label: "Design system", code: e.code, message: e.message });
    return out;
  },
  get hasProblems() {
    return this.problems.length > 0;
  },
  get problemsText() {
    const n = this.problems.length;
    return `${n} ${n === 1 ? "problem stops" : "problems stop"} this from saving. Fix the marked fields.`;
  },
  get tabs() {
    return TABS.map((t) => ({
      slug: t.slug,
      label: t.label,
      id: `design-system-tab-${t.slug}`,
      panelId: `design-system-panel-${t.slug}`,
      selected: t.slug === this.tab,
      hasCount: t.slug === "tokens",
      count: this.rows.length,
    }));
  },
  get visibleRows() {
    return filterRows(this.rows, this.filter, this.groupFilter);
  },
  get hasVisible() {
    return this.visibleRows.length > 0;
  },
  get noRows() {
    return this.rows.length === 0;
  },
  get noMatch() {
    return this.rows.length > 0 && this.visibleRows.length === 0;
  },
  get groups() {
    return groupCounts(this.rows);
  },
  get groupSuggestions() {
    return [...new Set([...this.groups.map((g) => g.group), ...STANDARD_GROUPS])];
  },
  get canApplyRename() {
    const group = this.renameGroup.trim();
    const name = this.renameName.trim();
    return !this.saving && name !== "" && group !== "" && `${group}.${name}` !== this.renameFrom;
  },

  isTab(slug) {
    return this.tab === slug;
  },

  init() {
    try {
      const seed = readSeed(this.$el);
      this.projectId = seed.project_id;
      this.base = seed.base;
      this.tab = tabForSlug(seed.tab ?? "").slug;
      if (seed.state) this.take(gateway.fromSeed(seed.state));
    } catch (err) {
      this.error = describeError(err);
      return;
    }
    globalThis.addEventListener("popstate", () => {
      this.tab = tabForSlug(new URLSearchParams(location.search).get("tab") ?? "").slug;
    });
    if (!this.loaded) this.load();
    this.unfollow = gateway.follow(this.projectId, {
      onSaved: (saved) => this.followed(saved),
      onStatus: (status) => {
        if (status === FeedStatus.RESYNCED) this.catchUp();
      },
    });
  },

  destroy() {
    this.unfollow?.();
  },

  async load() {
    try {
      this.take(await gateway.get(this.projectId));
      this.error = null;
    } catch (err) {
      this.error = describeError(err);
    }
  },

  /** Adopts a state from the server as the saved version and starts a fresh draft from it. */
  take(state) {
    this.saved = state.record;
    this.rows = rowsFromRecord(state.record);
    this.principles = state.record.principles;
    this.components = state.record.components;
    this.exists = state.exists;
    this.started = state.exists;
    this.version = state.version;
    this.updatedAt = state.updatedAt;
    this.updatedBy = state.updatedBy;
    this.loaded = true;
    this.message = "";
    this.cancelRename();
    this.clearProblems();
  },

  clearProblems() {
    this.rowErrors = {};
    this.otherErrors = [];
    this.conflict = "";
    this.elsewhere = 0;
  },

  /** A save announced on the project feed: a quiet reload when nothing is edited, a notice over edits. */
  followed({ version }) {
    if (this.saving) {
      // Probably our own save, but possibly another one that landed after it: keep the highest and judge once it answers.
      this.announced = Math.max(this.announced, version);
      return;
    }
    if (this.loaded && version <= this.version) return;
    if (!this.dirty) {
      this.load();
      return;
    }
    this.conflict = "feed";
    this.elsewhere = version;
  },

  /** After the feed reconnected: saves may have been missed, so ask what the server has. */
  async catchUp() {
    try {
      this.followed({ version: (await gateway.get(this.projectId)).version });
    } catch {
      // the feed will say again; the page stays as it was
    }
  },

  /** Drops the draft and loads what the server has. */
  async reload() {
    this.clearProblems();
    await this.load();
  },

  startBlank() {
    this.started = true;
    this.rows = [];
    this.openTab("tokens");
  },

  addToken() {
    const last = this.rows.findLast((r) => this.groupFilter === "" || r.group.trim() === this.groupFilter);
    const group = this.groupFilter || last?.group || STANDARD_GROUPS[0];
    const row = newRow(`n${++this.seq}`, group, last?.type ?? "color");
    this.rows.push(row);
    this.filter = "";
    this.$nextTick(() => document.querySelector(`[data-row-id="${row.id}"] [data-field=name]`)?.focus());
  },

  removeToken(row) {
    this.rows = this.rows.filter((r) => r.id !== row.id);
    this.touch(row);
    if (this.renamingId === row.id) this.cancelRename();
  },

  /** An edit to a row clears the errors the server named on it. */
  touch(row) {
    if (!this.rowErrors[row.id]) return;
    const { [row.id]: _gone, ...rest } = this.rowErrors;
    this.rowErrors = rest;
  },

  cellErrors(row, cell) {
    const fields = CELL_FIELDS[cell];
    return (this.rowErrors[row.id] ?? []).filter((e) => fields.includes(e.field)).map((e) => ({ key: `${e.field}:${e.code}`, code: e.code, message: e.message }));
  },

  isInvalid(row, cell) {
    return this.cellErrors(row, cell).length > 0;
  },

  sample(row) {
    return sampleOf(row, supports);
  },

  placeholder(row) {
    return placeholderFor(row.type);
  },

  isEdited(row) {
    return rowStatus(row, this.saved.tokens) !== "";
  },

  statusWord(row) {
    return rowStatus(row, this.saved.tokens);
  },

  pickGroup(group) {
    this.groupFilter = this.groupFilter === group ? "" : group;
  },

  isGroup(group) {
    return this.groupFilter === group;
  },

  canRename(row) {
    return row.origin !== null && !this.dirty && this.conflict === "" && !this.saving;
  },

  renameHint(row) {
    if (this.canRename(row)) return "Rename this token";
    return row.origin === null ? "Save the token first" : "Save or discard your edits first";
  },

  isRenaming(row) {
    return this.renamingId === row.id;
  },

  startRename(row) {
    if (!this.canRename(row)) return;
    this.renamingId = row.id;
    this.renameFrom = `${row.group}.${row.name}`;
    this.renameGroup = row.group;
    this.renameName = row.name;
    this.renameError = null;
    this.$nextTick(() => document.querySelector(`[data-row-id="${row.id}"] [data-field=new-name]`)?.focus());
  },

  cancelRename() {
    this.renamingId = "";
    this.renameError = null;
  },

  /** Renames the saved token as its own version, then takes what the server has. */
  async applyRename() {
    const row = this.rows.find((r) => r.id === this.renamingId);
    if (!row || !this.canApplyRename) return;
    const was = this.saved.tokens[row.origin];
    this.saving = true;
    this.announced = 0;
    this.renameError = null;
    try {
      await gateway.rename(this.projectId, this.version, { group: was.group, name: was.name }, { group: this.renameGroup.trim(), name: this.renameName.trim() });
      await this.load();
    } catch (err) {
      if (err?.code === CHANGED) {
        this.cancelRename();
        this.conflict = "save";
        this.elsewhere = Number(err.details?.current_version) || 0;
      } else if (err?.code === INVALID) {
        const first = invalidEntries(err.details)[0];
        this.renameError = { code: first?.code ?? err.code, message: first?.message ?? err.message };
      } else {
        this.error = describeError(err);
      }
    } finally {
      this.saving = false;
      this.judgeAnnounced();
    }
  },

  /** Saves the draft as the next version, against the version it started from. */
  async save() {
    if (!this.canSave) return;
    const rows = this.rows.slice();
    this.saving = true;
    this.announced = 0;
    this.rowErrors = {};
    this.otherErrors = [];
    try {
      await gateway.save(this.projectId, this.version, recordOf(rows, this.principles, this.components), this.message.trim());
      await this.load();
    } catch (err) {
      if (err?.code === CHANGED) {
        this.conflict = "save";
        this.elsewhere = Number(err.details?.current_version) || 0;
      } else if (err?.code === INVALID) {
        const sorted = sortErrors(invalidEntries(err.details), rows);
        this.rowErrors = sorted.byRow;
        this.otherErrors = sorted.other;
      } else {
        this.error = describeError(err);
      }
    } finally {
      this.saving = false;
      this.judgeAnnounced();
    }
  },

  /** A version beyond the one we hold was saved elsewhere while our own call was in flight. */
  judgeAnnounced() {
    const seen = this.announced;
    this.announced = 0;
    if (seen > this.version) this.followed({ version: seen });
  },

  /** Throws the draft away for the saved version. */
  discard() {
    if (this.conflict !== "") {
      this.reload();
      return;
    }
    this.rows = rowsFromRecord(this.saved);
    this.principles = this.saved.principles;
    this.message = "";
    this.cancelRename();
    this.clearProblems();
  },

  /** Opens a tab without leaving the page; the address follows it. */
  openTab(slug) {
    if (slug === this.tab) return;
    this.tab = slug;
    history.pushState(null, "", slug === TABS[0].slug ? this.base : `${this.base}?tab=${slug}`);
  },

  openHistory() {
    this.openTab("history");
  },
});
