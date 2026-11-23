/** The state of the live stream, as the panel shows it. "resynced": back after a drop, so what is on screen is read again. */
export type FeedStatus = "connecting" | "live" | "paused" | "resynced";
