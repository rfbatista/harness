// The thread component: what one conversation shows, in the panel and on the page. It holds
// no state of its own beyond what it announces: the conversation holds the thread.

import { Presenter } from "../../../../shared/kernel/presenter";
import type { Conversation, ConversationItem, ConversationState } from "../../application/conversation";
import type { TaskCatalog } from "../../application/ports";
import { parseMarkdown, type Block } from "../../domain/markdown";
import { isAnswering } from "../../domain/thread";

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
  /** An item this piece does not draw: a neutral line with its kind as a hidden label. */
  | { readonly kind: "other"; readonly key: string; readonly label: string; readonly text: string };

export interface LiveLine {
  readonly label: string;
  readonly step: string;
}

export interface ThreadViewModel {
  /** Counts the changes of what the thread shows: a view that follows the foot of the log scrolls on each. */
  readonly revision: number;
  readonly empty: { readonly heading: string; readonly lead: string; readonly starters: readonly string[] } | null;
  readonly rows: readonly RowViewModel[];
  /** What the product owner is doing now; null while it is idle. */
  readonly live: LiveLine | null;
  /** For a screen reader: "Product owner is answering", then "Answer finished". */
  readonly announcement: string;
  readonly loading: boolean;
  readonly composer: {
    readonly draft: string;
    readonly placeholder: string;
    readonly disabled: boolean;
    readonly mode: "send" | "stop";
    readonly canSend: boolean;
  };
  readonly error: { readonly code: string; readonly text: string; readonly fixUrl: string | null } | null;
}

const WHO = { person: "You", product_owner: "Product owner" } as const;

export class ThreadPresenter extends Presenter<ThreadViewModel> {
  readonly #conversation: Conversation;
  readonly #context: ThreadContext;
  readonly #tasks: TaskCatalog;
  #off: boolean;
  #announcement = "";
  #wasAnswering = false;
  #revision = 0;

  constructor(conversation: Conversation, context: ThreadContext, tasks: TaskCatalog, off = false) {
    super(emptyViewModel());
    this.#conversation = conversation;
    this.#context = context;
    this.#tasks = tasks;
    this.#off = off;
    conversation.subscribe((state) => this.set(this.#build(state)));
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
  dismissError = (): void => this.#conversation.dismissError();

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
    return {
      revision: ++this.#revision,
      empty: state.fresh && state.items.length === 0 && !state.loading ? this.#emptyState() : null,
      rows: state.items.flatMap((item, n) => row(item, n, state.items)),
      live: answering ? liveLine(state) : null,
      announcement: this.#announcement,
      loading: state.loading,
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

  #emptyState(): NonNullable<ThreadViewModel["empty"]> {
    const { projectName, noTasks } = this.#context;
    if (noTasks) return { heading: `${projectName} has no tasks yet`, lead: "Nothing to report yet. Tell me what you want to build and we shape it together.", starters: [FIRST_FEATURE_STARTER, ...STARTERS.slice(1, 3)] };
    return { heading: `Ask about ${projectName}`, lead: "I read the whole project: the board, the sessions and the documents.", starters: STARTERS };
  }
}

function emptyViewModel(): ThreadViewModel {
  return { revision: 0, empty: null, rows: [], live: null, announcement: "", loading: false, composer: { draft: "", placeholder: PLACEHOLDER_FRESH, disabled: false, mode: "send", canSend: false }, error: null };
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
