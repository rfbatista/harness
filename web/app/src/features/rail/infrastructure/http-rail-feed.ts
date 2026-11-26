// RailFeed over the project feed of the shared page stream.

import { DtoError } from "../../../shared/infrastructure/dto/dto-error";
import type { Feed } from "../../../shared/kernel/feed";
import type { RailChange, RailFeed } from "../application/ports";
import { SessionChangeDto, TicketChangeDto } from "./dto/project-change-dto";
import { toSession, toTask } from "./rail-mapper";

/** The project's feed, as the page event stream names it. */
export const projectFeedPath = (projectId: string): string => `/events?project_id=${encodeURIComponent(projectId)}`;

/** One feed message as the rail sees it, or null for a kind it does not know or cannot read (the next resync corrects it). */
export function toRailChange(data: unknown): RailChange | null {
  if (typeof data !== "object" || data === null) return null;
  try {
    if ("ticket" in data) {
      const dto = TicketChangeDto.fromJSON(data);
      return dto.deleted ? { kind: "task-deleted", id: dto.ticket.id } : { kind: "task-upsert", task: toTask(dto.ticket) };
    }
    if ("session" in data) {
      const dto = SessionChangeDto.fromJSON(data);
      return dto.deleted ? { kind: "deleted", id: dto.session.id } : { kind: "upsert", session: toSession(dto.session) };
    }
  } catch (err) {
    if (err instanceof DtoError) return null;
    throw err;
  }
  return null;
}

export function httpRailFeed(feed: Feed): RailFeed {
  return {
    follow: (projectId, { onChange, onStatus }) =>
      feed.follow(projectFeedPath(projectId), {
        onMessage(data) {
          const change = toRailChange(data);
          if (change) onChange(change);
        },
        ...(onStatus ? { onStatus } : {}),
      }),
  };
}
