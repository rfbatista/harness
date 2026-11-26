// MemorySource over GET /api/sessions/:id/memory.

import { ApiError, type ApiClient } from "../../../shared/infrastructure/api-client";
import { MemoryReadDto } from "../../../shared/infrastructure/dto/memory-dto";
import type { MemorySource } from "../../../shared/kernel/memory/ports";

export function httpMemorySource(api: ApiClient): MemorySource {
  return {
    async read(sessionId, signal) {
      try {
        return MemoryReadDto.fromJSON(await api.get(`/sessions/${encodeURIComponent(sessionId)}/memory`, undefined, signal));
      } catch (err) {
        if (err instanceof ApiError && err.status === 404) return null;
        throw err;
      }
    },
  };
}
