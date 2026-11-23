// What the notifications routes and the stream answer with: a page of
// notifications (GET /api/notifications), the notifications a read changed
// (POST /api/notifications/read) and one stream message (a change event).

import { array, count, object, optionalString, type Wire } from "../../../../shared/infrastructure/dto/fields";
import { NotificationDto } from "./notification-dto";

const PAGE = "NotificationPageDto";
const READ_ANSWER = "ReadAnswerDto";
const CHANGE = "ChangeEventDto";

/** GET /api/notifications → {notifications, unread_count, next_before?}. Also the page the island props carry. */
export class NotificationPageDto {
  constructor(
    readonly notifications: readonly NotificationDto[],
    readonly unread_count: number,
    readonly next_before: string | undefined,
  ) {}

  static fromJSON(raw: unknown, at = ""): NotificationPageDto {
    const from = object(PAGE, raw, at);
    return new NotificationPageDto(
      array(PAGE, from, "notifications", (item, path) => NotificationDto.fromJSON(item, path), at),
      count(PAGE, from, "unread_count", at),
      optionalString(PAGE, from, "next_before", at),
    );
  }

  toJSON(): Wire {
    return { notifications: this.notifications.map((n) => n.toJSON()), unread_count: this.unread_count, next_before: this.next_before };
  }
}

/** POST /api/notifications/read → {notifications: [changed…], unread_count}. */
export class ReadAnswerDto {
  constructor(
    readonly notifications: readonly NotificationDto[],
    readonly unread_count: number,
  ) {}

  static fromJSON(raw: unknown): ReadAnswerDto {
    const from = object(READ_ANSWER, raw);
    return new ReadAnswerDto(
      array(READ_ANSWER, from, "notifications", (item, path) => NotificationDto.fromJSON(item, path)),
      count(READ_ANSWER, from, "unread_count"),
    );
  }

  toJSON(): Wire {
    return { notifications: this.notifications.map((n) => n.toJSON()), unread_count: this.unread_count };
  }
}

/** One stream message on /api/notifications/events: {changed, unread_count}. */
export class ChangeEventDto {
  constructor(
    readonly changed: readonly NotificationDto[],
    readonly unread_count: number,
  ) {}

  static fromJSON(raw: unknown): ChangeEventDto {
    const from = object(CHANGE, raw);
    return new ChangeEventDto(
      array(CHANGE, from, "changed", (item, path) => NotificationDto.fromJSON(item, path)),
      count(CHANGE, from, "unread_count"),
    );
  }

  toJSON(): Wire {
    return { changed: this.changed.map((n) => n.toJSON()), unread_count: this.unread_count };
  }
}
