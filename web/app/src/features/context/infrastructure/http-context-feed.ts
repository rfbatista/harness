// ContextFeed over the project feed of the shared page stream.

import { DtoError } from "../../../shared/infrastructure/dto/dto-error";
import type { Feed } from "../../../shared/kernel/feed";
import type { ContextFeed } from "../application/ports";
import { ContextChangedDto } from "./dto/context-changed-dto";

/** The project's feed, as the page event stream names it. */
export const projectFeedPath = (projectId: string): string => `/events?project_id=${encodeURIComponent(projectId)}`;

export function httpContextFeed(feed: Feed): ContextFeed {
  return {
    follow: (projectId, onChanged) =>
      feed.follow(projectFeedPath(projectId), {
        onMessage(data) {
          let change: ContextChangedDto;
          try {
            change = ContextChangedDto.fromJSON(data);
          } catch (err) {
            // Any other event of the project feed. Anything but a DtoError is a bug.
            if (err instanceof DtoError) return;
            throw err;
          }
          onChanged({ sessionId: change.session_id, reason: change.reason });
        },
      }),
  };
}
