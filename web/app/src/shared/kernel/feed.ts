// A live feed: what an adapter over the page event stream offers.

/** "paused": dropped, retrying (what is on screen may be stale). "resynced": back after a drop, changes were missed. */
export type FeedStatus = "connecting" | "live" | "paused" | "resynced";

export interface FeedHandlers {
  onMessage(data: unknown): void;
  onStatus?(status: FeedStatus): void;
}

export interface Feed {
  /** Follows the stream path; returns the function that stops following it. */
  follow(path: string, handlers: FeedHandlers): () => void;
}
