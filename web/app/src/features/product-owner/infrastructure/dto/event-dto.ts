// What the project's event feed sends about its product owner (Contract: product owner, Events
// on the page stream). Each message is the feed's usual event object with one key set:
// { "product_owner_thread_item": { project_id, thread_id, item } } and so on. The feed also
// carries other events (tasks, sessions): a message with no product-owner key is not for this DTO.

import { object, string, type Wire } from "../../../../shared/infrastructure/dto/fields";
import { SettingsDto } from "./settings-dto";
import { ThreadItemDto, ThreadSummaryDto } from "./thread-dto";

const EVENT = "ProductOwnerEventDto";

export const EVENT_KEYS = ["product_owner_thread_item", "product_owner_delta", "product_owner_thread", "product_owner_settings"] as const;
export type EventKey = (typeof EVENT_KEYS)[number];

export class ProductOwnerEventDto {
  constructor(
    readonly key: EventKey,
    readonly project_id: string,
    readonly thread_id: string | undefined,
    readonly item: ThreadItemDto | undefined,
    readonly item_id: string | undefined,
    readonly text: string | undefined,
    readonly thread: ThreadSummaryDto | undefined,
    readonly settings: SettingsDto | undefined,
  ) {}

  /** null for a message that carries no product-owner event; a DtoError for one that carries a malformed one. */
  static fromJSON(raw: unknown): ProductOwnerEventDto | null {
    const from = object(EVENT, raw);
    const key = EVENT_KEYS.find((k) => from[k] !== undefined && from[k] !== null);
    if (key === undefined) return null;
    const body = object(EVENT, from[key], key);
    const project = string(EVENT, body, "project_id", key);
    const path = (field: string): string => `${key}.${field}`;
    switch (key) {
      case "product_owner_thread_item":
        return new ProductOwnerEventDto(key, project, string(EVENT, body, "thread_id", key), ThreadItemDto.fromJSON(body.item, path("item")), undefined, undefined, undefined, undefined);
      case "product_owner_delta":
        return new ProductOwnerEventDto(key, project, string(EVENT, body, "thread_id", key), undefined, string(EVENT, body, "item_id", key), string(EVENT, body, "text", key), undefined, undefined);
      case "product_owner_thread":
        return new ProductOwnerEventDto(key, project, undefined, undefined, undefined, undefined, ThreadSummaryDto.fromJSON(body.thread, path("thread")), undefined);
      case "product_owner_settings":
        return new ProductOwnerEventDto(key, project, undefined, undefined, undefined, undefined, undefined, SettingsDto.fromJSON(body.settings, path("settings")));
    }
  }

  toJSON(): Wire {
    switch (this.key) {
      case "product_owner_thread_item":
        return { [this.key]: { project_id: this.project_id, thread_id: this.thread_id, item: this.item?.toJSON() } };
      case "product_owner_delta":
        return { [this.key]: { project_id: this.project_id, thread_id: this.thread_id, item_id: this.item_id, text: this.text } };
      case "product_owner_thread":
        return { [this.key]: { project_id: this.project_id, thread: this.thread?.toJSON() } };
      case "product_owner_settings":
        return { [this.key]: { project_id: this.project_id, settings: this.settings?.toJSON() } };
    }
  }
}
