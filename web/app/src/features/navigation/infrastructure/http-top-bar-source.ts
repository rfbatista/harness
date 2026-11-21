// TopBarSource over GET /api/top_bar?path=.

import { ApiError, type ApiClient } from "../../../shared/infrastructure/api-client";
import { TopBarSourceError, type TopBarSource } from "../application/ports";
import { parseTopBar, TopBarError } from "../domain/top-bar";

export function httpTopBarSource(api: ApiClient): TopBarSource {
  return {
    async load(path, signal) {
      try {
        return parseTopBar(await api.get<unknown>("/top_bar", { path }, signal));
      } catch (err) {
        if (err instanceof TopBarError) throw new TopBarSourceError("BAD_RESPONSE", err.message);
        if (err instanceof ApiError) throw new TopBarSourceError(err.code === "INVALID_INPUT" || err.code === "NETWORK_ERROR" ? err.code : "UNKNOWN", err.message);
        throw err;
      }
    },
  };
}
