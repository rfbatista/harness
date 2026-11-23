// TopBarSource over GET /api/top_bar?path=.

import { ApiError, type ApiClient } from "../../../shared/infrastructure/api-client";
import { DtoError } from "../../../shared/infrastructure/dto/dto-error";
import { TopBarSourceError, type TopBarSource } from "../application/ports";
import { TopBarDto } from "./dto/top-bar-dto";
import { toTopBar } from "./top-bar-mapper";

export function httpTopBarSource(api: ApiClient): TopBarSource {
  return {
    async load(path, signal) {
      try {
        return toTopBar(TopBarDto.fromJSON(await api.get("/top_bar", { path }, signal)));
      } catch (err) {
        if (err instanceof DtoError) throw new TopBarSourceError("BAD_RESPONSE", err.message);
        if (err instanceof ApiError) throw new TopBarSourceError(err.code === "INVALID_INPUT" || err.code === "NETWORK_ERROR" || err.code === "BAD_RESPONSE" ? err.code : "UNKNOWN", err.message);
        throw err;
      }
    },
  };
}
