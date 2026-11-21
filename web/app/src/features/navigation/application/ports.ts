// What the navigation feature needs from the outside world.

import type { TopBar } from "../domain/top-bar";

export type TopBarSourceErrorCode = "INVALID_INPUT" | "NETWORK_ERROR" | "BAD_RESPONSE" | "UNKNOWN";

export class TopBarSourceError extends Error {
  readonly code: TopBarSourceErrorCode;

  constructor(code: TopBarSourceErrorCode, message: string) {
    super(message);
    this.name = "TopBarSourceError";
    this.code = code;
  }
}

/** Where the top-bar model for a page path comes from (GET /api/top_bar). Rejects with a TopBarSourceError. */
export interface TopBarSource {
  load(path: string, signal?: AbortSignal): Promise<TopBar>;
}

/** Takes the person to another page (a full page load). */
export interface PageOpener {
  open(url: string): void;
}
