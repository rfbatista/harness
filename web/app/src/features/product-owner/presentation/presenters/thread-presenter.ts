// The thread component: what one conversation shows, in the panel and on the page. It holds
// no state of its own beyond what it announces: the conversation holds the thread.

import { Presenter } from "../../../../shared/kernel/presenter";
import type { Conversation, ConversationItem, ConversationState } from "../../application/conversation";
import { memoryHref } from "../../application/links";
import type { TaskCatalog } from "../../application/ports";
import type { ProjectMemory } from "../../application/project-memory";
import { parseMarkdown, type Block } from "../../domain/markdown";
import { isAnswering, type MemoryOp } from "../../domain/thread";

export const STARTERS = ["What was done this week?", "What waits on me right now?", "What comes next, and why?", "Shape a new feature with me"] as const;
export const FIRST_FEATURE_STARTER = "Shape a first feature with me";
export const PLACEHOLDER_FRESH = "Ask about the project, or shape a feature";
export const PLACEHOLDER_CONTINUE = "Continue this thread";
export const OFF_PLACEHOLDER = "Turn the product owner on to send a message";

/** What the page tells of the project the thread belongs to. */
export interface ThreadContext {
  readonly projectName: string;
  readonly noTasks: boolean;
}

export type RowViewModel =
  | {
      readonly kind: "message";
      readonly key: string;
      readonly from: "person" | "product_owner";
      readonly who: string;
      readonly at: Date;
      readonly blocks: readonly Block[];
      /** The server has not answered for it yet. */
      readonly sending: boolean;
      readonly streaming: boolean;
      readonly stopped: boolean;
      readonly fromOtherTab: boolean;
    }
  | { readonly kind: "notice"; readonly key: string; readonly text: string }
  | { readonly kind: "failure"; readonly key: string; readonly code: string; readonly text: string; readonly retryItemId: string; readonly canRetry: boolean }
  /** The product owner saved, changed or dropped a memory entry. */
  | {
      readonly kind: "memory_note";
      readonly key: string;
      readonly op: MemoryOp;
      /** "Saved to memory", "Updated in memory" or "Dropped from memory". */
      readonly label: string;
      readonly text: string;
      readonly entryId: string;
      /** Where the entry is edited; null for a dropped entry, one that is gone, or while memory is unread. */
      readonly editHref: string | null;
    }
  /** An item this piece does not draw: a neutral line with its kind as a hidden label. */
  | { readonly kind: "other"; readonly key: string; readonly label: string; readonly text: string };

export interface LiveLine {
  readonly label: string;
  readonly step: string;
}

export interface ThreadViewModel {
  /** Counts the changes of what the thread shows: a view that follows the foot of the log scrolls on each. */
  readonly revision: number;
  readonly empty: { readonly heading: string; readonly lead: string; readonly starters: readonly string[]; readonly remembered: string } | null;
  readonly rows: readonly RowViewModel[];
  /** What the product owner is doing now; null while it is idle. */
  readonly live: LiveLine | null;
  /** For a screen reader: "Product owner is answering", then "Answer finished". */
  readonly announcement: string;
  readonly loading: boolean;
  /** Earlier items wait on the server: "Earlier messages" is offered. */
  readonly hasEarlier: boolean;
  readonly composer: {
    readonly draft: string;
    readonly placeholder: string;
    readonly disabled: boolean;
    readonly mode: "send" | "stop";
    readonly canSend: boolean;
  };
  readonly error: { readonly code: string; readonly text: string; readonly fixUrl: string | null } | null;
}

export const NOTE_LABEL: Readonly<Record<MemoryOp, string>> = { saved: "Saved to memory", changed: "Updated in memory", dropped: "Dropped from memory" };

/** What the thread knows of the project's memory: the entries, and what an Edit link does on this surface. */
export interface MemoryParts {
  readonly memory: ProjectMemory;
  /** On the page: the Memory tab opens at the entry in place. In the panel the link goes to the page. */
  readonly openEntry?: (entryId: string) => void;
}

const WHO = { person: "You", product_owner: "Product owner" } as const;

export class ThreadPresenter extends Presenter<ThreadViewModel> {
  readonly #conversation: Conversation;
  readonly #context: ThreadContext;
  readonly #tasks: TaskCatalog;
  readonly #memory: MemoryParts | undefined;
  #off: boolean;
  #notes = 0;
  #announcement = "";
  #wasAnswering = false;
  #revision = 0;
  #settled = false;

  constructor(conversation: Conversation, context: ThreadContext, tasks: TaskCatalog, off = false, memory?: MemoryParts) {
    super(emptyViewModel());
    this.#conversation = conversation;
    this.#context = context;
    this.#tasks = tasks;
    this.#off = off;
    this.#memory = memory;
    conversation.subscribe((state) => this.set(this.#build(state)));
    memory?.memory.subscribe(() => this.set(this.#build(this.#conversation.state)));
  }

  /** The product owner was switched off or on: the composer follows. */
  setOff(off: boolean): void {
    if (off === this.#off) return;
    this.#off = off;
    this.set(this.#build(this.#conversation.state));
  }

  setDraft = (draft: string): void => this.#conversation.setDraft(draft);
  send = (): Promise<void> => this.#conversation.send();
  stop = (): Promise<void> => this.#conversation.stop();
  startWith = (text: string): Promise<void> => this.#conversation.send(text);
  retry = (itemId: string): Promise<void> => {
    const item = this.#conversation.state.items.find((i) => i.itemId === itemId);
    return item?.kind === "message" ? this.#conversation.send(item.text) : Promise.resolve();
  };
  loadEarlier = (): Promise<void> => this.#conversation.loadEarlier();
  dismissError = (): void => this.#conversation.dismissError();

  /** The page opens the Memory tab in place; the panel does not. */
  get hasMemoryTab(): boolean {
    return this.#memory?.openEntry !== undefined;
  }

  /** An Edit link of a note, on the page: the Memory tab opens at the entry. */
  openMemoryEntry = (entryId: string): void => this.#memory?.openEntry?.(entryId);

  /** The status of a task named in an answer; "idle" for one the page does not know. */
  taskState = (taskId: string): "running" | "waiting" | "done" | "idle" => this.#tasks.find(taskId)?.state ?? "idle";

  /** Enter sends and Shift+Enter breaks a line; true when the key sent. */
  handleComposerKey = (key: { readonly key: string; readonly shiftKey: boolean; readonly isComposing?: boolean }): boolean => {
    if (key.key !== "Enter" || key.shiftKey || key.isComposing) return false;
    if (this.viewModel.composer.mode === "send" && this.viewModel.composer.canSend) void this.send();
    return true;
  };

  #build(state: ConversationState): ThreadViewModel {
    const answering = isAnswering(state.turn);
    if (answering !== this.#wasAnswering) this.#announcement = answering ? "Product owner is answering" : "Answer finished";
    this.#wasAnswering = answering;
    const draft = state.draft;
    const rows = state.items.flatMap((item, n) => this.#row(item, n, state));
    // A note that arrives while the thread is open is announced; the notes read with the thread are not.
    const notes = rows.filter((r) => r.kind === "memory_note");
    if (notes.length > this.#notes && this.#settled) this.#announcement = notes.at(-1)!.label;
    this.#notes = notes.length;
    this.#settled = !state.loading;
    return {
      revision: ++this.#revision,
      empty: state.fresh && state.items.length === 0 && !state.loading ? this.#emptyState() : null,
      rows,
      live: answering ? liveLine(state) : null,
      announcement: this.#announcement,
      loading: state.loading,
      hasEarlier: state.hasEarlier,
      composer: {
        draft,
        placeholder: this.#off ? OFF_PLACEHOLDER : state.fresh ? PLACEHOLDER_FRESH : PLACEHOLDER_CONTINUE,
        disabled: this.#off,
        mode: answering ? "stop" : "send",
        canSend: !this.#off && draft.trim() !== "",
      },
      error: state.error ? { code: state.error.code, text: state.error.text, fixUrl: state.error.fixUrl ?? null } : null,
    };
  }

  #row(item: ConversationItem, n: number, state: ConversationState): RowViewModel[] {
    if (item.kind !== "memory_note") return row(item, n, state.items);
    const mem = this.#memory?.memory.state;
    const there = item.op !== "dropped" && mem !== undefined && mem.loaded && mem.entries.some((e) => e.entryId === item.entryId);
    return [{ kind: "memory_note", key: item.itemId, op: item.op, label: NOTE_LABEL[item.op], text: item.text, entryId: item.entryId, editHref: there ? memoryHref(state.projectId, item.entryId) : null }];
  }

  /** "I remember 14 things we agreed": the thread starts informed. Nothing while none, or while unread. */
  #remembered(): string {
    const mem = this.#memory?.memory.state;
    const n = mem?.loaded ? mem.entries.length : 0;
    return n === 0 ? "" : `I remember ${n} ${n === 1 ? "thing" : "things"} we agreed`;
  }

  #emptyState(): NonNullable<ThreadViewModel["empty"]> {
    const { projectName, noTasks } = this.#context;
    if (noTasks) return { heading: `${projectName} has no tasks yet`, lead: "Nothing to report yet. Tell me what you want to build and we shape it together.", starters: [FIRST_FEATURE_STARTER, ...STARTERS.slice(1, 3)], remembered: this.#remembered() };
    return { heading: `Ask about ${projectName}`, lead: "I read the whole project: the board, the sessions and the documents.", starters: STARTERS, remembered: this.#remembered() };
  }
}

function emptyViewModel(): ThreadViewModel {
  return { revision: 0, empty: null, rows: [], live: null, announcement: "", loading: false, hasEarlier: false, composer: { draft: "", placeholder: PLACEHOLDER_FRESH, disabled: false, mode: "send", canSend: false }, error: null };
}

function row(item: ConversationItem, n: number, all: readonly ConversationItem[]): RowViewModel[] {
  switch (item.kind) {
    case "message":
      return [{ kind: "message", key: item.itemId, from: item.author, who: WHO[item.author], at: item.at, blocks: parseMarkdown(item.text), sending: item.sending === true, streaming: item.streaming, stopped: item.stopped, fromOtherTab: item.fromOtherTab === true }];
    case "notice":
      return [{ kind: "notice", key: item.itemId, text: item.text }];
    case "failure": {
      const retry = all.find((i) => i.itemId === item.retryItemId);
      // Only the last failure offers Try again: an older one has been answered by what came after it.
      const last = all.findLastIndex((i) => i.kind === "failure") === n;
      return [{ kind: "failure", key: item.itemId, code: item.code, text: item.text, retryItemId: item.retryItemId, canRetry: last && retry?.kind === "message" }];
    }
    case "other":
      return [{ kind: "other", key: item.itemId, label: item.wireKind.replace(/_/g, " "), text: item.text }];
    case "memory_note":
      return [];
    case "activity":
      // The live line shows the latest; the transcript does not keep a line per step.
      return [];
  }
}

function liveLine(state: ConversationState): LiveLine {
  if (state.turn === "resuming") return { label: "Resuming", step: "the thread was idle, its session wakes first" };
  if (state.turn === "interrupted") return { label: "Resuming", step: "the harness restarted, it continues where it stopped" };
  const last = state.items.at(-1);
  if (last?.kind === "message" && last.author === "product_owner" && last.streaming) return { label: "Writing", step: "" };
  const lastPerson = state.items.findLastIndex((i) => i.kind === "message" && i.author === "person");
  const activity = state.items.slice(lastPerson + 1).findLast((i) => i.kind === "activity");
  return { label: activity?.kind === "activity" ? activity.text : "Working", step: "" };
}
