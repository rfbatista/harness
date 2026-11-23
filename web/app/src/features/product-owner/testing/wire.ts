// Domain values as the server sends them, for the fake server and the stream tests.

import { ProductOwnerEventDto } from "../infrastructure/dto/event-dto";
import { SettingsDto } from "../infrastructure/dto/settings-dto";
import { ThreadHeadDto, ThreadItemDto, ThreadSummaryDto } from "../infrastructure/dto/thread-dto";
import type { ProductOwnerEvent, Settings, ThreadItem, ThreadSummary } from "../domain/thread";

export const settingsWire = (s: Settings): unknown => new SettingsDto(s.projectId, s.enabled).toJSON();

export const summaryWire = (t: ThreadSummary): unknown => new ThreadSummaryDto(t.threadId, t.title, t.startedAt, t.lastActivityAt, t.lastMessageExcerpt, t.titleSetByPerson, t.turn, t.pendingHeldActions).toJSON();

export const headWire = (h: { threadId: string; projectId: string; title: string; turn: ThreadSummary["turn"]; sessionId: string }): unknown => new ThreadHeadDto(h.threadId, h.projectId, h.title, h.turn, h.sessionId).toJSON();

export function itemWire(i: ThreadItem): unknown {
  const none = undefined;
  switch (i.kind) {
    case "message":
      return new ThreadItemDto(i.itemId, "message", i.at, i.author, i.text, i.streaming, i.stopped, none, none).toJSON();
    case "failure":
      return new ThreadItemDto(i.itemId, "failure", i.at, none, i.text, false, false, i.code, i.retryItemId).toJSON();
    case "other":
      return new ThreadItemDto(i.itemId, i.wireKind, i.at, none, i.text, false, false, none, none).toJSON();
    default:
      return new ThreadItemDto(i.itemId, i.kind, i.at, none, i.text, false, false, none, none).toJSON();
  }
}

/** The page-stream message for `event` of `projectId`. */
export function eventWire(projectId: string, e: ProductOwnerEvent): unknown {
  const raw = { project_id: projectId } as Record<string, unknown>;
  switch (e.type) {
    case "item":
      return { ...raw, type: "product_owner_thread_item", thread_id: e.threadId, item: itemWire(e.item) };
    case "delta":
      return { ...raw, type: "product_owner_delta", thread_id: e.threadId, item_id: e.itemId, text: e.text };
    case "thread":
      return { ...raw, type: "product_owner_thread", thread: summaryWire(e.thread) };
    case "settings":
      return { ...raw, type: "product_owner_settings", settings: settingsWire(e.settings) };
  }
}

export const parseEvent = (raw: unknown): ProductOwnerEventDto => ProductOwnerEventDto.fromJSON(raw);
