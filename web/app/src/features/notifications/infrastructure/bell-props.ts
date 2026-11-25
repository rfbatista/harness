// The bell's part of the island props as domain values.

import type { NotificationPage } from "../domain/notification";
import type { BellPropsDto } from "./dto/bell-props-dto";
import { toPage } from "./notification-mapper";

export interface BellProps {
  /** The inbox page. */
  readonly inboxHref: string;
  /** The page the server read, or null when it could not (the client then reads it). */
  readonly page: NotificationPage | null;
}

export function toBellProps(dto: BellPropsDto): BellProps {
  return { inboxHref: dto.inbox_href, page: dto.page ? toPage(dto.page) : null };
}
