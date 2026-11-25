// DTOs to the domain's values, and the other way for what the web UI sends.

import type { Digest, DigestPage } from "../domain/digest";
import type { DigestDto, DigestListDto } from "./dto/digest-dto";
import type { MemoryEntry, ProductOwnerEvent, Settings, ThreadHead, ThreadItem, ThreadPage, ThreadSummary } from "../domain/thread";
import type { ProductOwnerEventDto } from "./dto/event-dto";
import type { MemoryEntryDto } from "./dto/memory-dto";
import type { SettingsDto } from "./dto/settings-dto";
import type { ThreadItemDto, ThreadPageDto, ThreadSummaryDto } from "./dto/thread-dto";
import type { ThreadHeadDto } from "./dto/thread-dto";

export const toSettings = (dto: SettingsDto): Settings => ({ projectId: dto.project_id, enabled: dto.enabled, digestTime: dto.digest_time, timeZone: dto.time_zone, nextDigestAt: dto.next_digest_at });

export const toDigest = (dto: DigestDto): Digest => ({
  digestId: dto.digest_id,
  date: dto.date,
  periodStart: dto.period_start,
  periodEnd: dto.period_end,
  summary: dto.summary,
  items: dto.items.map((i) => ({ kind: i.kind, title: i.title, url: i.url, from: i.from ?? null, to: i.to ?? null })),
});

export const toDigestPage = (dto: DigestListDto): DigestPage => ({ digests: dto.digests.map(toDigest), hasEarlier: dto.has_earlier });

export const toSummary = (dto: ThreadSummaryDto): ThreadSummary => ({
  threadId: dto.thread_id,
  title: dto.title,
  startedAt: dto.started_at,
  lastActivityAt: dto.last_activity_at,
  lastMessageExcerpt: dto.last_message_excerpt,
  titleSetByPerson: dto.title_set_by_person,
  turn: dto.turn,
  pendingHeldActions: dto.pending_held_actions,
});

const toHead = (dto: ThreadHeadDto): ThreadHead => ({ threadId: dto.thread_id, projectId: dto.project_id, title: dto.title, turn: dto.turn, sessionId: dto.session_id });

export function toItem(dto: ThreadItemDto): ThreadItem {
  const base = { itemId: dto.item_id, at: dto.at };
  switch (dto.kind) {
    case "message":
      return { ...base, kind: "message", author: dto.author === "person" ? "person" : "product_owner", text: dto.text, streaming: dto.streaming, stopped: dto.stopped };
    case "activity":
      return { ...base, kind: "activity", text: dto.text };
    case "notice":
      return { ...base, kind: "notice", text: dto.text };
    case "failure":
      return { ...base, kind: "failure", code: dto.code ?? "", text: dto.text, retryItemId: dto.retry_item_id ?? "" };
    case "memory_note":
      return { ...base, kind: "memory_note", op: dto.op ?? "saved", entryId: dto.entry_id ?? "", text: dto.text };
    default:
      return { ...base, kind: "other", wireKind: dto.kind, text: dto.text };
  }
}

export const toEntry = (dto: MemoryEntryDto): MemoryEntry => ({ entryId: dto.entry_id, text: dto.text, savedAt: dto.saved_at, changedAt: dto.changed_at, changedBy: dto.changed_by, threadId: dto.thread_id, threadTitle: dto.thread_title });

export const toPage = (dto: ThreadPageDto): ThreadPage => ({ head: toHead(dto.thread), items: dto.items.map(toItem), hasEarlier: dto.has_earlier });

/** The event as the domain says it, or null for one whose parts are missing. */
export function toEvent(dto: ProductOwnerEventDto): ProductOwnerEvent | null {
  switch (dto.key) {
    case "product_owner_thread_item":
      return dto.thread_id !== undefined && dto.item ? { type: "item", threadId: dto.thread_id, item: toItem(dto.item) } : null;
    case "product_owner_delta":
      return dto.thread_id !== undefined && dto.item_id !== undefined && dto.text !== undefined ? { type: "delta", threadId: dto.thread_id, itemId: dto.item_id, text: dto.text } : null;
    case "product_owner_thread":
      return dto.thread ? { type: "thread", thread: toSummary(dto.thread) } : null;
    case "product_owner_settings":
      return dto.settings ? { type: "settings", settings: toSettings(dto.settings) } : null;
    case "product_owner_digest":
      return dto.digest_id !== undefined && dto.date !== undefined ? { type: "digest", digestId: dto.digest_id, date: dto.date } : null;
    case "product_owner_memory":
      return dto.entry_id !== undefined && dto.op !== undefined ? { type: "memory", entryId: dto.entry_id, op: dto.op } : null;
  }
}
