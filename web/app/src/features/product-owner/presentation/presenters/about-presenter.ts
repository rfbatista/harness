// "About this thread": the facts of the open thread, renaming it, its link, and the way to
// the Context tab that shows the agent, prompt and model it started with (ADR 0099).

import { Presenter } from "../../../../shared/kernel/presenter";
import type { Conversation, ConversationState } from "../../application/conversation";
import { contextHref, threadHref } from "../../application/links";
import type { ThreadList } from "../../application/thread-list";

export interface AboutViewModel {
  /** False on a fresh thread: there is nothing to tell yet. */
  readonly available: boolean;
  readonly title: string;
  /** "Mon 23 Nov 2026, 14:02", or "" while it is not known. */
  readonly started: string;
  /** "12 messages", "100+ messages" while earlier ones wait. */
  readonly messages: string;
  /** The thread's own URL, as a path. */
  readonly link: string;
  /** The thread's session Context tab; null while the session is not known. */
  readonly contextHref: string | null;
  readonly error: string;
}

const MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];
const WEEKDAYS = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];
const two = (n: number): string => String(n).padStart(2, "0");

export const startedText = (at: Date): string => `${WEEKDAYS[at.getDay()]} ${at.getDate()} ${MONTHS[at.getMonth()]} ${at.getFullYear()}, ${two(at.getHours())}:${two(at.getMinutes())}`;

export class AboutPresenter extends Presenter<AboutViewModel> {
  readonly #conversation: Conversation;
  readonly #list: ThreadList;
  #unsubscribe: Array<() => void> = [];

  constructor(conversation: Conversation, list: ThreadList) {
    super(about(conversation.state, list));
    this.#conversation = conversation;
    this.#list = list;
    this.#unsubscribe = [conversation.subscribe((s) => this.set(about(s, this.#list))), list.subscribe(() => this.set(about(this.#conversation.state, this.#list)))];
  }

  destroy(): void {
    for (const stop of this.#unsubscribe) stop();
    this.#unsubscribe = [];
  }

  /** Resolves true when the title was accepted. */
  rename = (title: string): Promise<boolean> => this.#conversation.rename(title);
}

function about(state: ConversationState, list: ThreadList): AboutViewModel {
  const summary = list.state.threads.find((t) => t.threadId === state.threadId);
  const count = state.items.filter((i) => i.kind === "message").length;
  return {
    available: state.threadId !== null,
    title: state.title,
    started: summary ? startedText(summary.startedAt) : "",
    messages: `${count}${state.hasEarlier ? "+" : ""} ${count === 1 && !state.hasEarlier ? "message" : "messages"}`,
    link: state.threadId === null ? "" : threadHref(state.projectId, state.threadId),
    contextHref: state.threadId !== null && state.sessionId !== "" ? contextHref(state.projectId, state.sessionId) : null,
    error: state.error?.code === "INVALID_INPUT" ? state.error.text : "",
  };
}
