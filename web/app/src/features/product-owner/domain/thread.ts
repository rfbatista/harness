// The product owner's values: a project's switch, its threads and what a thread holds
// (Contract: Harness server ↔ Web UI — product owner, Threads and Thread items).

export const TURNS = ["idle", "resuming", "running", "interrupted"] as const;
/** `resuming`: the thread's session wakes first. `interrupted`: cut off by a restart, being resumed. */
export type Turn = (typeof TURNS)[number];

/** A turn the product owner is still on: the thread answers. */
export const isAnswering = (turn: Turn): boolean => turn !== "idle";

export interface Settings {
  readonly projectId: string;
  readonly enabled: boolean;
  /** "HH:MM" in the server's time zone; null while no digest time is set: no digest is then recorded. */
  readonly digestTime: string | null;
  /** The server's time zone, read-only. */
  readonly timeZone: string;
}

export interface ThreadSummary {
  readonly threadId: string;
  readonly title: string;
  readonly startedAt: Date;
  readonly lastActivityAt: Date;
  /** The first line of the last message. */
  readonly lastMessageExcerpt: string;
  readonly titleSetByPerson: boolean;
  readonly turn: Turn;
  readonly pendingHeldActions: number;
}

/** The thread as one page of it names it. */
export interface ThreadHead {
  readonly threadId: string;
  readonly projectId: string;
  readonly title: string;
  readonly turn: Turn;
  /** The thread's session: only the Context tab link uses it. */
  readonly sessionId: string;
}

export interface MessageItem {
  readonly kind: "message";
  readonly itemId: string;
  readonly at: Date;
  readonly author: "person" | "product_owner";
  readonly text: string;
  /** The answer is still arriving. */
  readonly streaming: boolean;
  /** The person stopped the turn: the text is partial. */
  readonly stopped: boolean;
}

export interface ActivityItem {
  readonly kind: "activity";
  readonly itemId: string;
  readonly at: Date;
  readonly text: string;
}

export interface NoticeItem {
  readonly kind: "notice";
  readonly itemId: string;
  readonly at: Date;
  readonly text: string;
}

export interface FailureItem {
  readonly kind: "failure";
  readonly itemId: string;
  readonly at: Date;
  readonly code: string;
  readonly text: string;
  /** The person's message to send again. */
  readonly retryItemId: string;
}

/** An item this piece does not draw (held action, memory note, checkpoint): a neutral line. */
export interface OtherItem {
  readonly kind: "other";
  /** The wire kind, for the line's hidden label. */
  readonly wireKind: string;
  readonly itemId: string;
  readonly at: Date;
  readonly text: string;
}

export type ThreadItem = MessageItem | ActivityItem | NoticeItem | FailureItem | OtherItem;

export interface ThreadPage {
  readonly head: ThreadHead;
  /** Oldest first. */
  readonly items: readonly ThreadItem[];
  readonly hasEarlier: boolean;
}

/** What the page stream says about a project's product owner. */
export type ProductOwnerEvent =
  | { readonly type: "item"; readonly threadId: string; readonly item: ThreadItem }
  | { readonly type: "delta"; readonly threadId: string; readonly itemId: string; readonly text: string }
  | { readonly type: "thread"; readonly thread: ThreadSummary }
  | { readonly type: "settings"; readonly settings: Settings }
  /** A digest was recorded, or changed, for `date`. */
  | { readonly type: "digest"; readonly digestId: string; readonly date: string };
