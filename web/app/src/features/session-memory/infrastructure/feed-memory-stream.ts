// MemoryStream over a session's own stream (/sessions/:id/events on the page's shared feed). It carries every
// event of the session; only the two memory kinds are read.

import { DtoError } from "../../../shared/infrastructure/dto/dto-error";
import { MemoryKillDto, MemorySampleDto } from "../../../shared/infrastructure/dto/memory-dto";
import type { Feed } from "../../../shared/kernel/feed";
import type { MemoryStream } from "../../../shared/kernel/memory/ports";

export const sessionStreamPath = (sessionId: string): string => `/sessions/${encodeURIComponent(sessionId)}/events`;

const kindOf = (data: unknown): unknown => (typeof data === "object" && data !== null ? (data as { kind?: unknown }).kind : undefined);

export function feedMemoryStream(feed: Feed): MemoryStream {
  return {
    follow: (sessionId, handlers) =>
      feed.follow(sessionStreamPath(sessionId), {
        onMessage(data) {
          try {
            const kind = kindOf(data);
            if (kind === "session.memory") handlers.onSample(MemorySampleDto.fromJSON(data).toDomain());
            else if (kind === "session.memory_killed") handlers.onKill(MemoryKillDto.fromJSON(data).toDomain());
          } catch (err) {
            if (!(err instanceof DtoError)) throw err;
          }
        },
        onStatus(status) {
          if (status === "resynced") handlers.onResync();
        },
      }),
  };
}
