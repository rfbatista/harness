import type { CurrentTopBar } from "./current-top-bar";
import { loadTopBar } from "./load-top-bar";
import type { NavigationSignal, TopBarSource } from "./ports";

export interface FollowOptions {
  /**
   * The top bar was just inserted by a swap that brought fresh props: the
   * navigation event of that swap is not asked for (the props are the answer).
   * A top bar of the first page load has no such event to skip, and a swap of
   * <main> only keeps the top bar, so the same path asks again.
   */
  readonly fresh?: boolean;
}

/**
 * Keeps the top bar on screen in step with the page: on every navigation it
 * reads the model for the new path (the same path too: the page's state can
 * change without its path). An answer that a later navigation overtook is
 * dropped, and a failed read keeps the last model. A path already being asked
 * for is not asked for again (a Back is both a popstate and an in-place
 * navigation). Returns the unsubscribe.
 */
export function followNavigation(signal: NavigationSignal, source: TopBarSource, current: CurrentTopBar, { fresh = false }: FollowOptions = {}): () => void {
  let skip: string | null = fresh ? current.get().path : null;
  let asking: { path: string; controller: AbortController } | null = null;
  const stop = signal.subscribe((path) => {
    const skipped = path === skip;
    skip = null;
    if (skipped) return;
    if (asking?.path === path) return;
    asking?.controller.abort();
    const request = { path, controller: new AbortController() };
    asking = request;
    const done = () => {
      if (asking === request) asking = null;
    };
    loadTopBar(source, path, request.controller.signal).then(
      (topBar) => {
        if (asking === request) current.set(topBar);
        done();
      },
      done, // keep the last model; the person's next move asks again
    );
  });
  return () => {
    stop();
    asking?.controller.abort();
  };
}
