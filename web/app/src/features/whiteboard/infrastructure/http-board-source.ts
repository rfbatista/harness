// BoardSource over the harness HTTP API and the page event stream.

import { ApiError, type ApiClient } from "../../../shared/infrastructure/api-client";
import { DtoError } from "../../../shared/infrastructure/dto/dto-error";
import { BoardSourceError, type BoardSource } from "../application/ports";
import { toSession } from "./board-mapper";
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
  };
}
