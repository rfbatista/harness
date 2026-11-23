// BoardSource over the harness HTTP API and the page event stream.

import { ApiError, type ApiClient } from "../../../shared/infrastructure/api-client";
import { DtoError } from "../../../shared/infrastructure/dto/dto-error";
import { BoardSourceError, type BoardSource } from "../application/ports";
import { toMessage, toReview, toSession } from "./board-mapper";
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

export function httpBoardSource(api: ApiClient): BoardSource {
  return {
    sessions: (ticketId, signal) => mapErrors(async () => BoardSessionListDto.fromJSON(await api.get("/sessions", { ticket_id: ticketId }, signal)).sessions.map(toSession)),
    messages: (ticketId, signal) => mapErrors(async () => TaskMessageListDto.fromJSON(await api.get("/task_messages", { ticket_id: ticketId }, signal)).messages.map(toMessage)),
    pendingReviews: (ticketId, signal) =>
      mapErrors(async () => ReviewRequestListDto.fromJSON(await api.get("/review_requests", { ticket_id: ticketId, state: "pending" }, signal)).review_requests.map(toReview)),
  };
}
