// BoardSource over the harness HTTP API and the page event stream.

import { ApiError, type ApiClient } from "../../../shared/infrastructure/api-client";
import { DtoError } from "../../../shared/infrastructure/dto/dto-error";
import type { Feed } from "../../../shared/kernel/feed";
import { BoardSourceError, type BoardSource } from "../application/ports";
import { toEvent, toMessage, toReview, toSession } from "./board-mapper";
import { BoardEventDto } from "./dto/event-dto";
import { ReviewRequestListDto, TaskMessageListDto } from "./dto/message-dto";
import { BoardSessionListDto } from "./dto/session-dto";

/** Every adapter call fails with a BoardSourceError. */
export async function mapErrors<T>(call: () => Promise<T>): Promise<T> {
  try {
    return await call();
  } catch (err) {
    if (err instanceof DtoError) throw new BoardSourceError("BAD_RESPONSE", err.message);
    if (err instanceof ApiError) throw new BoardSourceError(err.code === "NETWORK_ERROR" || err.code === "BAD_RESPONSE" ? err.code : "UNKNOWN", err.message);
    throw err;
  }
}

/** The project feed as the page event stream names it. */
export const projectFeedPath = (projectId: string): string => `/events?project_id=${encodeURIComponent(projectId)}`;

export function httpBoardSource(api: ApiClient, feed: Feed): BoardSource {
  return {
    sessions: (ticketId, signal) => mapErrors(async () => BoardSessionListDto.fromJSON(await api.get("/sessions", { ticket_id: ticketId }, signal)).sessions.map(toSession)),
    messages: (ticketId, signal) => mapErrors(async () => TaskMessageListDto.fromJSON(await api.get("/task_messages", { ticket_id: ticketId }, signal)).messages.map(toMessage)),
    pendingReviews: (ticketId, signal) =>
      mapErrors(async () => ReviewRequestListDto.fromJSON(await api.get("/review_requests", { ticket_id: ticketId, state: "pending" }, signal)).review_requests.map(toReview)),
    follow: (projectId, follower) =>
      feed.follow(projectFeedPath(projectId), {
        onMessage(data) {
          try {
            const event = toEvent(BoardEventDto.fromJSON(data));
            if (event) follower.onEvent(event);
          } catch (err) {
            // A message that does not read is dropped; the next resync corrects the board. Anything else is a bug.
            if (!(err instanceof DtoError)) throw err;
          }
        },
        onStatus: (status) => follower.onStatus(status),
      }),
  };
}
