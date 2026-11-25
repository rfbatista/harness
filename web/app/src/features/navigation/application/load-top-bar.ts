import type { TopBar } from "../domain/top-bar";
import type { TopBarSource } from "./ports";

/** Reads the top-bar model for a page path. */
export function loadTopBar(source: TopBarSource, path: string, signal?: AbortSignal): Promise<TopBar> {
  return source.load(path, signal);
}
