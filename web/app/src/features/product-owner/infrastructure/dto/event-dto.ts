// What the page stream sends about a project's product owner (Contract: product owner,
// Events on the page stream). Each message names its event in `type`.

import { object, oneOf, string, type Wire } from "../../../../shared/infrastructure/dto/fields";
import { SettingsDto } from "./settings-dto";
import { ThreadItemDto, ThreadSummaryDto } from "./thread-dto";

const EVENT = "ProductOwnerEventDto";

export const EVENT_TYPES = ["product_owner_thread_item", "product_owner_delta", "product_owner_thread", "product_owner_settings"] as const;

export class ProductOwnerEventDto {
  constructor(
    readonly type: (typeof EVENT_TYPES)[number],
    readonly project_id: string,
    readonly thread_id: string | undefined,
    readonly item: ThreadItemDto | undefined,
    readonly item_id: string | undefined,
    readonly text: string | undefined,
    readonly thread: ThreadSummaryDto | undefined,
    readonly settings: SettingsDto | undefined,
  ) {}

  static fromJSON(raw: unknown): ProductOwnerEventDto {
    const from = object(EVENT, raw);
    const type = oneOf(EVENT, from, "type", EVENT_TYPES);
    const project = string(EVENT, from, "project_id");
    if (type === "product_owner_thread_item") return new ProductOwnerEventDto(type, project, string(EVENT, from, "thread_id"), ThreadItemDto.fromJSON(from.item, "item"), undefined, undefined, undefined, undefined);
    if (type === "product_owner_delta") return new ProductOwnerEventDto(type, project, string(EVENT, from, "thread_id"), undefined, string(EVENT, from, "item_id"), string(EVENT, from, "text"), undefined, undefined);
    if (type === "product_owner_thread") return new ProductOwnerEventDto(type, project, undefined, undefined, undefined, undefined, ThreadSummaryDto.fromJSON(from.thread, "thread"), undefined);
    return new ProductOwnerEventDto(type, project, undefined, undefined, undefined, undefined, undefined, SettingsDto.fromJSON(from.settings, "settings"));
  }

  toJSON(): Wire {
    const base = { type: this.type, project_id: this.project_id };
    if (this.type === "product_owner_thread_item") return { ...base, thread_id: this.thread_id, item: this.item?.toJSON() };
    if (this.type === "product_owner_delta") return { ...base, thread_id: this.thread_id, item_id: this.item_id, text: this.text };
    if (this.type === "product_owner_thread") return { ...base, thread: this.thread?.toJSON() };
    return { ...base, settings: this.settings?.toJSON() };
  }
}
