// The bell's part of the island props as domain values.

import { DtoError } from "../../../shared/infrastructure/dto/dto-error";
import type { NotificationPage } from "../domain/notification";
import { BellPropsDto } from "./dto/bell-props-dto";
import { toPage } from "./notification-mapper";

export interface BellProps {
  /** The inbox page. */
  readonly inboxHref: string;
  /** The page the server read, or null when it could not (the client then reads it). */
  readonly page: NotificationPage | null;
}

/** `raw` is the model's `notifications` part. Null when the inbox is not served or its part cannot be read: no bell. */
export function toBellProps(raw: unknown): BellProps | null {
  if (raw === null || raw === undefined) return null;
  try {
    const dto = BellPropsDto.fromJSON(raw);
    return { inboxHref: dto.inbox_href, page: dto.page ? toPage(dto.page) : null };
  } catch (err) {
    if (!(err instanceof DtoError)) throw err;
    // A page that cannot be read is no page: the first read fills the count. Anything else, and there is no bell.
    if (err.path !== "page" && !err.path.startsWith("page.") && !err.path.startsWith("page[")) return null;
    const href = (raw as { inbox_href?: unknown }).inbox_href;
    return typeof href === "string" ? { inboxHref: href, page: null } : null;
  }
}
