// ScoreFeed over the project feed of the shared page stream: the bench run changes.

import { DtoError } from "../../../shared/infrastructure/dto/dto-error";
import type { Feed } from "../../../shared/kernel/feed";
import type { ScoreFeed } from "../application/ports";
import type { RunChange } from "../domain/bench";
import { RunChangeDto } from "./dto/bench-dto";
import { toRunChange } from "./score-mapper";

const projectFeedPath = (projectId: string): string => `/events?project_id=${encodeURIComponent(projectId)}`;

/** One feed message as a run change, or null for any other kind or one that cannot be read. */
export function toRunChangeOrNull(data: unknown): RunChange | null {
  if (typeof data !== "object" || data === null || !("bench_run" in data)) return null;
  try {
    return toRunChange(RunChangeDto.fromJSON(data));
  } catch (err) {
    if (err instanceof DtoError) return null;
    throw err;
  }
}

export function httpScoreFeed(feed: Feed): ScoreFeed {
  return {
    follow: (projectId, { onRun, onStatus }) =>
      feed.follow(projectFeedPath(projectId), {
        onMessage(data) {
          const change = toRunChangeOrNull(data);
          if (change) onRun(change);
        },
        ...(onStatus ? { onStatus } : {}),
      }),
  };
}
