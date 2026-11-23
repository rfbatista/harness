// The island props of the top bar: the top-bar model (Contract: Harness server
// ↔ Web UI — the top bar island, §1), read once for both composition roots. It
// is composed of the features' DTOs, each reading its own part.

import { DtoError } from "../shared/infrastructure/dto/dto-error";
import { TopBarDto } from "../features/navigation/infrastructure/dto/top-bar-dto";
import { BellPropsDto } from "../features/notifications/infrastructure/dto/bell-props-dto";
import type { Wire } from "../shared/infrastructure/dto/fields";

export class TopBarPropsDto {
  constructor(
    readonly topBar: TopBarDto,
    /** Null when the server does not serve the inbox, or its part cannot be read: there is no bell. */
    readonly notifications: BellPropsDto | null,
  ) {}

  /** Throws a DtoError when the navigation part cannot be read. A bell part that cannot be read leaves the bar without a bell, and a bell page that cannot be read leaves the count to a first read. */
  static fromJSON(raw: unknown): TopBarPropsDto {
    const topBar = TopBarDto.fromJSON(raw);
    const part = (raw as Wire).notifications;
    return new TopBarPropsDto(topBar, part === null || part === undefined ? null : bell(part));
  }

  toJSON(): Wire {
    return { ...this.topBar.toJSON(), notifications: this.notifications?.toJSON() ?? null };
  }
}

function bell(part: unknown): BellPropsDto | null {
  try {
    return BellPropsDto.fromJSON(part);
  } catch (err) {
    if (!(err instanceof DtoError)) throw err;
    const href = typeof part === "object" && part !== null ? (part as Wire).inbox_href : undefined;
    // Only the page failed: the bell stays, and reads its own count.
    return err.path.startsWith("page") && typeof href === "string" ? new BellPropsDto(href, null) : null;
  }
}
