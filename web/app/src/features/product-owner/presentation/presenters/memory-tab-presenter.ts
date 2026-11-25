// The Memory tab of the product owner page: the project's entries, newest first, with a filter.
// Each entry shows when it was saved, the thread it came from and who edited it last.

import { Presenter } from "../../../../shared/kernel/presenter";
import { threadHref } from "../../application/links";
import type { ProjectMemory, ProjectMemoryState } from "../../application/project-memory";
import { ProductOwnerError } from "../../application/ports";
import { MAX_ENTRY, entryLength, type MemoryEntry } from "../../domain/thread";

export interface EntryRow {
  readonly kind: "entry";
  readonly key: string;
  readonly entryId: string;
  readonly text: string;
  /** "Saved Wed 18 Nov, 09:20". */
  readonly saved: string;
  /** The thread it came from; "" when the entry has none. */
  readonly threadId: string;
  readonly threadTitle: string;
  readonly threadHref: string | null;
  /** "edited by you Fri 13 Nov", or "". */
  readonly edited: string;
  /** It arrived after the tab was read. */
  readonly fresh: boolean;
  readonly mode: "view" | "edit" | "delete";
  /** The draft, while the entry is in edit. */
  readonly edit: EditViewModel | null;
  /** Why the delete did not go through. */
  readonly deleteError: string;
}

export interface EditViewModel {
  readonly draft: string;
  /** In Unicode code points, as the server counts. */
  readonly length: number;
  readonly limit: number;
  /** Near the limit: the count shows. */
  readonly showCount: boolean;
  readonly over: boolean;
  readonly canSave: boolean;
  readonly saving: boolean;
  readonly error: string;
}

/** Where the view moves focus; `seq` counts the requests, so asking again for the same place still moves it. */
export interface FocusRequest {
  readonly entryId: string;
  readonly target: "text" | "edit-button" | "delete-button" | "delete-confirm" | "receipt" | "entry" | "";
  readonly seq: number;
}

export interface ReceiptRow {
  readonly kind: "receipt";
  readonly key: string;
  /** `Deleted "…" · 14:21`. */
  readonly text: string;
}

export type MemoryRow = EntryRow | ReceiptRow;

export interface MemoryTabViewModel {
  readonly loading: boolean;
  readonly error: string;
  /** How many entries the project has, whatever the filter says. */
  readonly count: number;
  readonly sentence: string;
  readonly filter: string;
  readonly rows: readonly MemoryRow[];
  /** Read, and nothing remembered. */
  readonly empty: boolean;
  /** Entries exist, and the filter hides all of them. */
  readonly noMatch: boolean;
  readonly focus: FocusRequest;
}

const MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];
const WEEKDAYS = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];
const two = (n: number): string => String(n).padStart(2, "0");
const day = (at: Date): string => `${WEEKDAYS[at.getDay()]} ${at.getDate()} ${MONTHS[at.getMonth()]}`;
const clock = (at: Date): string => `${two(at.getHours())}:${two(at.getMinutes())}`;

const SENTENCE = "The product owner reads all of them at the start of every thread. Fix anything that is wrong or stale; it counts from the next message.";

const SHOW_COUNT_AT = 3500;

interface Draft {
  readonly entryId: string;
  readonly text: string;
  readonly saving: boolean;
  readonly error: string;
}

interface Ui {
  readonly filter: string;
  readonly draft: Draft | null;
  /** The entry asking "Delete this entry?", and why the last try failed. */
  readonly deleting: { readonly entryId: string; readonly error: string } | null;
  readonly focus: FocusRequest;
}

export class MemoryTabPresenter extends Presenter<MemoryTabViewModel> {
  readonly #memory: ProjectMemory;
  readonly #projectId: string;
  readonly #stop: () => void;
  #ui: Ui;
  /** Entries whose delete is under way: a second click on Delete entry does not send another. */
  readonly #deleting = new Set<string>();

  /** `focusEntry`: the page was opened at this entry, and brings it into view. */
  constructor(memory: ProjectMemory, projectId: string, focusEntry = "") {
    const ui: Ui = { filter: "", draft: null, deleting: null, focus: { entryId: focusEntry, target: focusEntry === "" ? "" : "entry", seq: focusEntry === "" ? 0 : 1 } };
    super(build(memory.state, projectId, ui));
    this.#memory = memory;
    this.#projectId = projectId;
    this.#ui = ui;
    this.#stop = memory.subscribe((state) => {
      // The entry being edited or deleted was dropped: its edit or confirm goes with it.
      const gone = (id: string): boolean => state.loaded && !state.entries.some((e) => e.entryId === id);
      const draft = this.#ui.draft !== null && gone(this.#ui.draft.entryId) ? null : this.#ui.draft;
      const deleting = this.#ui.deleting !== null && gone(this.#ui.deleting.entryId) ? null : this.#ui.deleting;
      this.#ui = { ...this.#ui, draft, deleting };
      this.set(build(state, this.#projectId, this.#ui));
    });
  }

  destroy(): void {
    this.#stop();
  }

  setFilter = (filter: string): void => this.#update({ filter });

  retry = (): void => this.#memory.retry();

  /** Edit: one entry at a time; a confirm elsewhere closes. */
  startEdit = (entryId: string): void => {
    const entry = this.#memory.state.entries.find((e) => e.entryId === entryId);
    if (entry === undefined) return;
    this.#update({ draft: { entryId, text: entry.text, saving: false, error: "" }, deleting: null, focus: this.#focus(entryId, "text") });
  };

  setDraft = (text: string): void => {
    const draft = this.#ui.draft;
    if (draft !== null) this.#update({ draft: { ...draft, text, error: "" } });
  };

  cancel = (): void => {
    const draft = this.#ui.draft;
    if (draft !== null) this.#update({ draft: null, focus: this.#focus(draft.entryId, "edit-button") });
  };

  save = async (): Promise<void> => {
    const draft = this.#ui.draft;
    if (draft === null || draft.saving || !canSave(draft.text)) return;
    this.#update({ draft: { ...draft, saving: true, error: "" } });
    try {
      await this.#memory.update(draft.entryId, draft.text);
      if (this.#ui.draft?.entryId === draft.entryId) this.#update({ draft: null, focus: this.#focus(draft.entryId, "edit-button") });
    } catch (err) {
      if (!(err instanceof ProductOwnerError)) throw err;
      // Not found: the entry already left the list, and the subscription closed its edit.
      if (this.#ui.draft?.entryId === draft.entryId) this.#update({ draft: { ...draft, saving: false, error: err.message } });
    }
  };

  /** A note in a thread asked for this entry: it comes into view. */
  showEntry = (entryId: string): void => this.#update({ focus: this.#focus(entryId, "entry") });

  /** The view moved focus as asked: a later visit to the tab does not move it again. */
  focused = (): void => {
    if (this.#ui.focus.target !== "") this.#update({ focus: { ...this.#ui.focus, target: "" } });
  };

  askDelete = (entryId: string): void => this.#update({ deleting: { entryId, error: "" }, draft: null, focus: this.#focus(entryId, "delete-confirm") });

  keep = (): void => {
    const deleting = this.#ui.deleting;
    if (deleting !== null) this.#update({ deleting: null, focus: this.#focus(deleting.entryId, "delete-button") });
  };

  confirmDelete = async (): Promise<void> => {
    const deleting = this.#ui.deleting;
    if (deleting === null || this.#deleting.has(deleting.entryId)) return;
    this.#deleting.add(deleting.entryId);
    try {
      await this.#memory.remove(deleting.entryId);
      // A receipt stands only for an entry the list still held; with none there is nothing to focus.
      const receipt = this.#memory.state.receipts.some((r) => r.entryId === deleting.entryId);
      this.#update({ deleting: null, ...(receipt ? { focus: this.#focus(deleting.entryId, "receipt") } : {}) });
    } catch (err) {
      if (!(err instanceof ProductOwnerError)) throw err;
      this.#update({ deleting: { entryId: deleting.entryId, error: err.message } });
    } finally {
      this.#deleting.delete(deleting.entryId);
    }
  };

  #focus(entryId: string, target: FocusRequest["target"]): FocusRequest {
    return { entryId, target, seq: this.#ui.focus.seq + 1 };
  }

  #update(patch: Partial<Ui>): void {
    this.#ui = { ...this.#ui, ...patch };
    this.set(build(this.#memory.state, this.#projectId, this.#ui));
  }
}

const canSave = (text: string): boolean => text.trim() !== "" && entryLength(text) <= MAX_ENTRY;

function entryRow(e: MemoryEntry, projectId: string, state: ProjectMemoryState, ui: Ui): EntryRow {
  const by = e.changedBy === "person" ? "you" : "the product owner";
  return {
    kind: "entry",
    key: e.entryId,
    entryId: e.entryId,
    text: e.text,
    saved: `Saved ${day(e.savedAt)}, ${clock(e.savedAt)}`,
    threadId: e.threadId,
    threadTitle: e.threadTitle === "" ? "a thread" : e.threadTitle,
    threadHref: e.threadId === "" ? null : threadHref(projectId, e.threadId),
    edited: e.changedAt === null || e.changedBy === null ? "" : `edited by ${by} ${day(e.changedAt)}`,
    fresh: state.fresh.has(e.entryId),
    mode: ui.draft?.entryId === e.entryId ? "edit" : ui.deleting?.entryId === e.entryId ? "delete" : "view",
    edit: ui.draft?.entryId === e.entryId ? editView(ui.draft, e.text) : null,
    deleteError: ui.deleting?.entryId === e.entryId ? ui.deleting.error : "",
  };
}

function editView(draft: Draft, original: string): EditViewModel {
  const length = entryLength(draft.text);
  return { draft: draft.text, length, limit: MAX_ENTRY, showCount: length >= SHOW_COUNT_AT, over: length > MAX_ENTRY, canSave: canSave(draft.text) && draft.text !== original && !draft.saving, saving: draft.saving, error: draft.error };
}

function build(state: ProjectMemoryState, projectId: string, ui: Ui): MemoryTabViewModel {
  const filter = ui.filter;
  const count = state.entries.length;
  const needle = filter.trim().toLowerCase();
  const shown = needle === "" ? state.entries : state.entries.filter((e) => e.text.toLowerCase().includes(needle));
  const rows: MemoryRow[] = shown.map((e) => entryRow(e, projectId, state, ui));
  if (needle === "") {
    // A receipt stands where its entry stood.
    for (const r of state.receipts) rows.splice(Math.min(r.index, rows.length), 0, { kind: "receipt", key: `receipt-${r.entryId}`, text: `Deleted "${r.text}" · ${clock(r.at)}` });
  }
  return {
    loading: state.loading && !state.loaded,
    error: state.error,
    count,
    sentence: `${count} ${count === 1 ? "entry" : "entries"}. ${SENTENCE}`,
    filter,
    rows,
    empty: state.loaded && count === 0 && state.receipts.length === 0,
    noMatch: count > 0 && needle !== "" && shown.length === 0,
    focus: ui.focus,
  };
}
