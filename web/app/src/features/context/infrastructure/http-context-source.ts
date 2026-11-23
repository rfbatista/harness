// ContextSource over GET /api/sessions/{id}/context and the full-piece endpoint.

import { ApiError, type ApiClient } from "../../../shared/infrastructure/api-client";
import { DtoError } from "../../../shared/infrastructure/dto/dto-error";
import { ContextSourceError, type ContextSource, type ContextSourceErrorCode } from "../application/ports";
import { PieceTextDto, SessionContextDto } from "./dto/session-context-dto";
import { toSessionContext } from "./session-context-mapper";

const KNOWN: readonly string[] = ["SESSION_NOT_FOUND", "CONTEXT_ENTRY_NOT_FOUND", "CONTEXT_PIECE_NOT_FOUND", "NETWORK_ERROR", "BAD_RESPONSE"];

/** Every adapter call fails with a ContextSourceError. */
async function mapErrors<T>(call: () => Promise<T>): Promise<T> {
  try {
    return await call();
  } catch (err) {
    if (err instanceof DtoError) throw new ContextSourceError("BAD_RESPONSE", err.message);
    if (err instanceof ApiError) {
      throw new ContextSourceError(KNOWN.includes(err.code) ? (err.code as ContextSourceErrorCode) : "UNKNOWN", err.message);
    }
    throw err;
  }
}

const enc = encodeURIComponent;

export function httpContextSource(api: ApiClient): ContextSource {
  return {
    load: (sessionId, signal) => mapErrors(async () => toSessionContext(SessionContextDto.fromJSON(await api.get(`/sessions/${enc(sessionId)}/context`, undefined, signal)))),
    piece: (sessionId, entryId, pieceId, signal) =>
      mapErrors(async () => PieceTextDto.fromJSON(await api.get(`/sessions/${enc(sessionId)}/context/entries/${enc(entryId)}/pieces/${enc(pieceId)}`, undefined, signal)).text),
  };
}
