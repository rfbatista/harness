// Threads and their items (Contract: product owner, Threads and Thread items).

import { DtoError } from "../../../../shared/infrastructure/dto/dto-error";
import { array, boolean, object, oneOf, optionalString, string, time, count, type Wire } from "../../../../shared/infrastructure/dto/fields";
import { MEMORY_OPS, TURNS } from "../../domain/thread";

const SUMMARY = "ThreadSummaryDto";
const HEAD = "ThreadHeadDto";
const ITEM = "ThreadItemDto";
const PAGE = "ThreadPageDto";
const LIST = "ThreadListDto";

export class ThreadSummaryDto {
  constructor(
    readonly thread_id: string,
    readonly title: string,
    readonly started_at: Date,
    readonly last_activity_at: Date,
    readonly last_message_excerpt: string,
    readonly title_set_by_person: boolean,
    readonly turn: (typeof TURNS)[number],
    readonly pending_held_actions: number,
  ) {}

  static fromJSON(raw: unknown, at = ""): ThreadSummaryDto {
    const from = object(SUMMARY, raw, at);
    return new ThreadSummaryDto(
      string(SUMMARY, from, "thread_id", at),
      string(SUMMARY, from, "title", at),
      time(SUMMARY, from, "started_at", at),
      time(SUMMARY, from, "last_activity_at", at),
      optionalString(SUMMARY, from, "last_message_excerpt", at) ?? "",
      from.title_set_by_person === undefined ? false : boolean(SUMMARY, from, "title_set_by_person", at),
      oneOf(SUMMARY, from, "turn", TURNS, at),
      from.pending_held_actions === undefined ? 0 : count(SUMMARY, from, "pending_held_actions", at),
    );
  }

  toJSON(): Wire {
    return {
      thread_id: this.thread_id,
      title: this.title,
      started_at: this.started_at.toISOString(),
      last_activity_at: this.last_activity_at.toISOString(),
      last_message_excerpt: this.last_message_excerpt,
      title_set_by_person: this.title_set_by_person,
      turn: this.turn,
      pending_held_actions: this.pending_held_actions,
    };
  }
}

/** GET /api/product_owner_threads. */
export class ThreadListDto {
  constructor(readonly threads: readonly ThreadSummaryDto[]) {}

  static fromJSON(raw: unknown): ThreadListDto {
    return new ThreadListDto(array(LIST, object(LIST, raw), "threads", (item, at) => ThreadSummaryDto.fromJSON(item, at)));
  }

  toJSON(): Wire {
    return { threads: this.threads.map((t) => t.toJSON()) };
  }
}

export class ThreadHeadDto {
  constructor(
    readonly thread_id: string,
    readonly project_id: string,
    readonly title: string,
    readonly turn: (typeof TURNS)[number],
    readonly session_id: string,
  ) {}

  static fromJSON(raw: unknown, at = ""): ThreadHeadDto {
    const from = object(HEAD, raw, at);
    return new ThreadHeadDto(string(HEAD, from, "thread_id", at), string(HEAD, from, "project_id", at), string(HEAD, from, "title", at), oneOf(HEAD, from, "turn", TURNS, at), string(HEAD, from, "session_id", at));
  }

  toJSON(): Wire {
    return { thread_id: this.thread_id, project_id: this.project_id, title: this.title, turn: this.turn, session_id: this.session_id };
  }
}

/** The kinds that carry their sentence in a field of their own, and that this piece draws as a neutral line. */
const NEUTRAL: Readonly<Record<string, string>> = { held_action: "summary", checkpoint: "summary" };

/** One thread item. Only the fields of its kind are set. */
export class ThreadItemDto {
  constructor(
    readonly item_id: string,
    readonly kind: string,
    readonly at: Date,
    /** message: "person" | "product_owner". */
    readonly author: string | undefined,
    /** The kind's sentence: a message's Markdown, an activity, notice or failure text, a neutral item's summary. */
    readonly text: string,
    readonly streaming: boolean,
    readonly stopped: boolean,
    readonly code: string | undefined,
    readonly retry_item_id: string | undefined,
    /** memory_note: what happened to the entry, and which one. */
    readonly op: (typeof MEMORY_OPS)[number] | undefined = undefined,
    readonly entry_id: string | undefined = undefined,
  ) {}

  static fromJSON(raw: unknown, at = ""): ThreadItemDto {
    const from = object(ITEM, raw, at);
    const kind = string(ITEM, from, "kind", at);
    const id = string(ITEM, from, "item_id", at);
    const when = time(ITEM, from, "at", at);
    const path = (key: string): string => (at === "" ? key : `${at}.${key}`);
    if (kind === "message") {
      const author = oneOf(ITEM, from, "author", ["person", "product_owner"] as const, at);
      const flag = (key: string): boolean => (from[key] === undefined ? false : boolean(ITEM, from, key, at));
      return new ThreadItemDto(id, kind, when, author, string(ITEM, from, "text", at), flag("streaming"), flag("stopped"), undefined, undefined);
    }
    if (kind === "activity" || kind === "notice") return new ThreadItemDto(id, kind, when, undefined, string(ITEM, from, "text", at), false, false, undefined, undefined);
    if (kind === "failure") return new ThreadItemDto(id, kind, when, undefined, string(ITEM, from, "text", at), false, false, string(ITEM, from, "code", at), string(ITEM, from, "retry_item_id", at));
    if (kind === "memory_note") return new ThreadItemDto(id, kind, when, undefined, string(ITEM, from, "text", at), false, false, undefined, undefined, oneOf(ITEM, from, "op", MEMORY_OPS, at), string(ITEM, from, "entry_id", at));
    const field = NEUTRAL[kind];
    if (field !== undefined) return new ThreadItemDto(id, kind, when, undefined, string(ITEM, from, field, at), false, false, undefined, undefined);
    throw new DtoError(ITEM, path("kind"), `unknown item kind "${kind}"`);
  }

  toJSON(): Wire {
    const base = { item_id: this.item_id, kind: this.kind, at: this.at.toISOString() };
    if (this.kind === "message") return { ...base, author: this.author, text: this.text, attachments: [], streaming: this.streaming, stopped: this.stopped };
    if (this.kind === "failure") return { ...base, code: this.code, text: this.text, retry_item_id: this.retry_item_id };
    if (this.kind === "memory_note") return { ...base, op: this.op, entry_id: this.entry_id, text: this.text };
    const field = NEUTRAL[this.kind];
    return { ...base, [field ?? "text"]: this.text };
  }
}

/** GET /api/product_owner_thread. */
export class ThreadPageDto {
  constructor(
    readonly thread: ThreadHeadDto,
    readonly items: readonly ThreadItemDto[],
    readonly has_earlier: boolean,
  ) {}

  static fromJSON(raw: unknown): ThreadPageDto {
    const from = object(PAGE, raw);
    return new ThreadPageDto(ThreadHeadDto.fromJSON(from.thread, "thread"), array(PAGE, from, "items", (item, at) => ThreadItemDto.fromJSON(item, at)), boolean(PAGE, from, "has_earlier"));
  }

  toJSON(): Wire {
    return { thread: this.thread.toJSON(), items: this.items.map((i) => i.toJSON()), has_earlier: this.has_earlier };
  }
}
