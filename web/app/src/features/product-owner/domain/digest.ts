// A project's daily digest (Contract: product owner, Digests): what moved, what ended and what
// waits on the person, for the day of one digest time.

export type DigestItemKind = "task_moved" | "session_ended" | "session_failed" | "waiting_on_person";

export interface DigestItem {
  readonly kind: DigestItemKind;
  readonly title: string;
  /** A page of the web UI. */
  readonly url: string;
  /** A task moved from this status; only on `task_moved`. */
  readonly from: string | null;
  readonly to: string | null;
}

export interface Digest {
  readonly digestId: string;
  /** The local date, "YYYY-MM-DD", of the digest time it covers; at most one digest per date. */
  readonly date: string;
  readonly periodStart: Date;
  readonly periodEnd: Date;
  /** Markdown. */
  readonly summary: string;
  readonly items: readonly DigestItem[];
}

/** At most 30 digests, the newest first. */
export interface DigestPage {
  readonly digests: readonly Digest[];
  readonly hasEarlier: boolean;
}

export const DIGEST_PAGE_SIZE = 30;
