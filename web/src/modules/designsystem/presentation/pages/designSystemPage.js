// The page of one design system of a project: one draft of the whole record, edited by the
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
import { renderMarkdown } from "../../../../shared/domain/markdown.js";
import { VERSIONS_PAGE, hasMore, mergeVersions, versionRow } from "../../domain/history.js";
import { outlineOf } from "../../domain/principles.js";
import { previewDocument, usableTokens } from "../../domain/preview.js";
import { cardsOf, currentOf, itemsOf, notesOf } from "../../domain/showcase.js";
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
  tokenOf,
} from "../../domain/tokens.js";

/** The fields of a row an error is shown under, by the cell that holds them. */
const CELL_FIELDS = { group: ["group"], name: ["name", "row"], value: ["value", "type"] };

/** Whether the browser reads a CSS value; true where it cannot tell. */
const supports = (property, value) => globalThis.CSS?.supports?.(property, value) ?? true;

/** @param {{ gateway: ReturnType<typeof import("../../infrastructure/designsystem-gateway.js").designSystemGateway>, clock: import("../../../../shared/infrastructure/clock.js").Clock, clipboard?: { copyText: (text: string) => Promise<boolean> }, history?: History, location?: Location }} deps */
export const designSystemPage = ({ gateway, clock, clipboard = { copyText: async () => false }, history = globalThis.history, location = globalThis.location }) => () => ({
  projectId: "",
  designSystemId: "",
  name: "",
  slug: "",
  description: "",
  /** The design system was deleted (its change arrived and it is gone). */
  deleted: false,
  base: "",
  tab: TABS[0].slug,
  loaded: false,
  error: null,
  /** Whether a version was ever saved, and the one the page holds. */
  exists: false,
  version: 0,
  updatedAt: "",
  updatedBy: "",
  /** Start blank was pressed on a design system with no version: the record is open, nothing is stored. */
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
  /** The Components tab: the component open (its slug; "" for the first) and the previews that have come near the viewport, by card key. */
  selectedSlug: "",
  seen: {},
  /** The Principles tab: Write or Preview. */
  principlesMode: "write",
  /** The History tab: the versions read (newest first), whether the first page was read, a read in flight (and one asked for meanwhile), whether older ones may exist, and what went wrong. */
  versions: [],
  historyLoaded: false,
  historyBusy: false,
  historyAgain: false,
  moreVersions: false,
  historyError: null,
  /** The version open for reading (null: the list), and what copying last said. */
  opened: null,
  copiedNote: "",
  unfollow: null,

  get isFirstStart() {
    return this.loaded && !this.deleted && !this.exists && !this.started;
  },
  get isOpen() {
    return this.loaded && !this.deleted && (this.exists || this.started);
  },
  get hasDescription() {
    return this.description !== "";
  },
  get startBlankText() {
    return `An empty record for “${this.name}”, ready for its first tokens.`;
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
      hasCount: t.slug === "tokens" || t.slug === "components",
      count: t.slug === "components" ? this.components.length : this.rows.length,
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

  /** The tokens previews are drawn from: the draft's, saved or not. */
  get previewTokens() {
    return usableTokens(this.rows.map(tokenOf));
  },
  get componentItems() {
    return itemsOf(this.components, this.previewTokens);
  },
  get hasComponents() {
    return this.components.length > 0;
  },
  get noComponents() {
    return this.components.length === 0;
  },
  get current() {
    return currentOf(this.components, this.selectedSlug);
  },
  get currentCards() {
    return this.current ? cardsOf(this.current, this.previewTokens) : [];
  },
  get currentDos() {
    return notesOf(this.current?.do ?? []);
  },
  get currentDonts() {
    return notesOf(this.current?.dont ?? []);
  },
  get currentMissing() {
    return this.componentItems.find((i) => i.slug === this.current?.slug)?.missing.join(", ") ?? "";
  },
  get currentFlagged() {
    return this.currentMissing !== "";
  },
  get outline() {
    return outlineOf(this.principles);
  },
  get hasOutline() {
    return this.outline.length > 0;
  },
  get writeShown() {
    return this.principlesMode === "write";
  },
  get previewShown() {
    return this.principlesMode === "preview";
  },
  get writePressed() {
    return String(this.writeShown);
  },
  get previewPressed() {
    return String(this.previewShown);
  },
  get versionRows() {
    const now = clock.now();
    return this.versions.map((v) => versionRow(v, now));
  },
  get hasVersions() {
    return this.versions.length > 0;
  },
  get hasMoreVersions() {
    return this.moreVersions;
  },
  get hasHistoryError() {
    return this.historyError !== null;
  },
  get viewing() {
    return this.opened !== null;
  },
  get openedLabel() {
    return `Version ${this.opened?.version}`;
  },
  get openedRows() {
    return this.opened?.rows ?? [];
  },
  get openedHasPrinciples() {
    return (this.opened?.principles ?? "").trim() !== "";
  },
  get hasCopiedNote() {
    return this.copiedNote !== "";
  },
  isTab(slug) {
    return this.tab === slug;
  },

  init() {
    try {
      const seed = readSeed(this.$el);
      this.projectId = seed.project_id;
      this.designSystemId = seed.design_system_id;
      this.name = seed.name ?? "";
      this.slug = seed.slug ?? "";
      this.description = seed.description ?? "";
      this.base = seed.base;
      this.tab = tabForSlug(seed.tab ?? "").slug;
      if (seed.state) this.take(gateway.fromSeed(seed.state));
    } catch (err) {
      this.error = describeError(err);
      return;
    }
    globalThis.addEventListener("popstate", () => {
      this.tab = tabForSlug(new URLSearchParams(location.search).get("tab") ?? "").slug;
      if (this.tab === "history") this.loadVersions();
    });
    if (!this.loaded) this.load();
    if (this.tab === "history") this.loadVersions();
    this.unfollow = gateway.follow(this.projectId, this.designSystemId, {
      onSaved: (saved) => this.followed(saved),
      onChanged: () => this.identityChanged(),
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
      this.take(await gateway.get(this.designSystemId));
      this.error = null;
    } catch (err) {
      this.error = describeError(err);
    }
  },

  /** Adopts a state from the server as the saved version and starts a fresh draft from it. */
  take(state) {
    if (this.historyLoaded && state.version > (this.versions[0]?.version ?? 0)) this.loadVersions();
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
    if (this.historyLoaded && version > (this.versions[0]?.version ?? 0)) this.loadVersions();
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

  /** The design system's name or description changed, or it was deleted: read what it is now. */
  async identityChanged() {
    try {
      const now = await gateway.identity(this.designSystemId);
      if (now === null) {
        this.deleted = true;
        return;
      }
      this.name = now.name;
      this.description = now.description;
    } catch {
      // the feed will say again; the header stays as it was
    }
  },

  /** After the feed reconnected: saves may have been missed, so ask what the server has. */
  async catchUp() {
    try {
      this.followed({ version: (await gateway.get(this.designSystemId)).version });
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
      await gateway.rename(this.designSystemId, this.version, { group: was.group, name: was.name }, { group: this.renameGroup.trim(), name: this.renameName.trim() });
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
      await gateway.save(this.designSystemId, this.version, recordOf(rows, this.principles, this.components), this.message.trim());
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

  isCurrent(item) {
    return item.slug === this.current?.slug;
  },

  selectComponent(item) {
    this.selectedSlug = item.slug;
  },

  /** The document a preview is drawn from; none until the card nears the viewport, so a page of many previews stays light. */
  previewSrc(card) {
    return this.seen[card.key] ? previewDocument(this.previewTokens, card.preview) : "";
  },

  /**
   * Draws a card's preview in a frame inside its host. The frame has
   * sandbox="" (no scripts, no same origin) and is built here because the CSP
   * build of Alpine refuses directives on an iframe.
   */
  paintFrame(host, card) {
    const doc = this.previewSrc(card);
    if (doc === "") return;
    let frame = host.querySelector("iframe");
    if (!frame) {
      frame = document.createElement("iframe");
      frame.setAttribute("sandbox", "");
      frame.setAttribute("data-preview-frame", "");
      frame.setAttribute("title", card.label);
      frame.style.cssText = "display:block;inline-size:100%;block-size:8rem;border:0;background:var(--color-canvas)";
      host.append(frame);
    }
    if (frame.srcdoc !== doc) frame.srcdoc = doc;
  },

  /** Marks a card's frame as wanted once it comes near the viewport. */
  watchFrame(el, card) {
    const want = () => {
      this.seen[card.key] = true;
    };
    if (!globalThis.IntersectionObserver) {
      want();
      return;
    }
    const observer = new IntersectionObserver(
      (entries) => {
        if (entries.some((e) => e.isIntersecting)) {
          want();
          observer.disconnect();
        }
      },
      { rootMargin: "200px" },
    );
    observer.observe(el);
  },

  /** Opens a tab without leaving the page; the address follows it. */
  openTab(slug) {
    if (slug === this.tab) return;
    this.tab = slug;
    if (slug === "history") this.loadVersions();
    history.pushState(null, "", slug === TABS[0].slug ? this.base : `${this.base}?tab=${slug}`);
  },

  openHistory() {
    this.openTab("history");
  },

  showWrite() {
    this.principlesMode = "write";
  },

  showPreview() {
    this.principlesMode = "preview";
  },

  /** The principles' textarea. */
  principlesField() {
    return document.querySelector("#design-system-panel-principles [data-field=principles]");
  },

  /** Back in Write, puts the cursor at the start of an outline section and scrolls it to the top of the field. */
  jumpTo(section, area = this.principlesField(), lineHeight = parseFloat(getComputedStyle(area).lineHeight) || 20) {
    this.principlesMode = "write";
    this.$nextTick(() => {
      area.focus();
      area.setSelectionRange(section.offset, section.offset);
      area.scrollTop = section.line * lineHeight;
    });
  },

  /** Puts the principles, as Markdown, into the preview's element; nothing while the preview is hidden. The text is escaped on the way. */
  paintPrinciples(el) {
    el.innerHTML = this.previewShown ? renderMarkdown(this.principles) : "";
  },

  /** Puts the opened version's principles, as Markdown, into its element. */
  paintOpenedPrinciples(el) {
    el.innerHTML = this.opened ? renderMarkdown(this.opened.principles) : "";
  },

  /** Reads the first page of the list and joins it to what is held: the first read, a save here or a save announced. */
  async loadVersions() {
    if (this.historyBusy) {
      this.historyAgain = true;
      return;
    }
    this.historyBusy = true;
    this.historyAgain = false;
    try {
      const fresh = await gateway.versions(this.designSystemId, { limit: VERSIONS_PAGE });
      this.versions = mergeVersions(this.versions, fresh);
      if (!this.historyLoaded) this.moreVersions = hasMore(fresh);
      this.historyLoaded = true;
      this.historyError = null;
    } catch (err) {
      this.historyError = describeError(err);
    } finally {
      this.historyBusy = false;
      if (this.historyAgain) this.loadVersions();
    }
  },

  /** Reads the versions older than the oldest held. */
  async loadMoreVersions() {
    const oldest = this.versions[this.versions.length - 1]?.version;
    if (!oldest || this.historyBusy) return;
    this.historyBusy = true;
    try {
      const older = await gateway.versions(this.designSystemId, { limit: VERSIONS_PAGE, before: oldest });
      this.versions = [...this.versions, ...older];
      this.moreVersions = hasMore(older);
      this.historyError = null;
    } catch (err) {
      this.historyError = describeError(err);
    } finally {
      this.historyBusy = false;
    }
  },

  /** Opens a version for reading: its tokens and principles, read only. */
  async openVersion(number) {
    this.copiedNote = "";
    try {
      const v = await gateway.version(this.designSystemId, number);
      this.opened = { version: v.version, at: v.at, rows: rowsFromRecord(v.record), principles: v.record.principles, components: v.record.components.length };
      this.historyError = null;
    } catch (err) {
      this.historyError = describeError(err);
    }
  },

  closeVersion() {
    this.opened = null;
    this.copiedNote = "";
  },

  /** The row's value on the clipboard. */
  async copyValue(row) {
    await this.copied(row.text, `Copied ${row.group}.${row.name}`);
  },

  async copyOpenedPrinciples() {
    await this.copied(this.opened.principles, "Copied the principles");
  },

  async copied(text, note) {
    this.copiedNote = (await clipboard.copyText(text)) ? note : "Could not copy: the browser refused";
  },
});
