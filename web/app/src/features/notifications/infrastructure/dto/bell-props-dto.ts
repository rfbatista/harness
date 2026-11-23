// The `notifications` part of the top-bar model, as the island props carry it
// (Contract: Harness server ↔ Web UI — the top bar island, §1).

import { object, string, type Wire } from "../../../../shared/infrastructure/dto/fields";
import { NotificationPageDto } from "./notification-page-dto";

const NAME = "BellPropsDto";

export class BellPropsDto {
  constructor(
    readonly inbox_href: string,
    /** The API's page, or null when the server could not read it (the client then reads it). */
    readonly page: NotificationPageDto | null,
  ) {}

  static fromJSON(raw: unknown): BellPropsDto {
    const from = object(NAME, raw);
    return new BellPropsDto(string(NAME, from, "inbox_href"), from.page === null || from.page === undefined ? null : NotificationPageDto.fromJSON(from.page, "page"));
  }

  toJSON(): Wire {
    return { inbox_href: this.inbox_href, page: this.page?.toJSON() ?? null };
  }
}
