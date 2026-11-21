// TopBarSource in memory, for tests and for development without a server. It
// answers like the server: a path that matches no page is not an error, and a
// path that is not an absolute URL path is INVALID_INPUT.

import { TopBarSourceError, type TopBarSource } from "../application/ports";
import type { TopBar } from "../domain/top-bar";

export interface MemoryTopBarSource extends TopBarSource {
  /** What the server answers for `path`. Unserved paths answer `fallback(path)`. */
  serve(path: string, topBar: TopBar): void;
  /** The next loads fail with this error until `heal()`. */
  failWith(error: TopBarSourceError): void;
  heal(): void;
  /** Holds the answers for `path` until the returned function releases them. */
  hold(path: string): () => void;
  /** The paths asked for, in order. */
  readonly asked: readonly string[];
}

export function memoryTopBarSource(fallback: (path: string) => TopBar): MemoryTopBarSource {
  const served = new Map<string, TopBar>();
  const asked: string[] = [];
  const holds = new Map<string, Promise<void>>();
  let failure: TopBarSourceError | null = null;
  return {
    asked,
    serve: (path, topBar) => void served.set(path, topBar),
    failWith: (error) => void (failure = error),
    heal: () => void (failure = null),
    hold(path) {
      let release = () => {};
      holds.set(
        path,
        new Promise<void>((resolve) => {
          release = () => {
            holds.delete(path);
            resolve();
          };
        }),
      );
      return release;
    },
    async load(path, signal) {
      signal?.throwIfAborted();
      asked.push(path);
      if (!path.startsWith("/")) throw new TopBarSourceError("INVALID_INPUT", "path must be an absolute URL path");
      if (failure) throw failure;
      const held = holds.get(path);
      if (held) await held;
      signal?.throwIfAborted();
      return served.get(path) ?? { ...fallback(path), path };
    },
  };
}
