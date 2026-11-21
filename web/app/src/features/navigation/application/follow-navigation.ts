import type { CurrentTopBar } from "./current-top-bar";
import { loadTopBar } from "./load-top-bar";
import type { NavigationSignal, TopBarSource } from "./ports";

/**
 * Keeps the top bar on screen in step with the page: on every navigation it
 * reads the model for the new path (the same path too: the page's state can
 * change without its path). An answer that a later navigation overtook is
 * dropped, and a failed read keeps the last model. A path already being asked
 * for is not asked for again (a Back is both a popstate and an in-place
 * navigation), and neither is the path the top bar was built for, once: the
 * swap that brought fresh props announces itself too. Returns the unsubscribe.
 */
export function followNavigation(signal: NavigationSignal, source: TopBarSource, current: CurrentTopBar): () => void {
  let fresh: string | null = current.get().path;
  let asking: { path: string; controller: AbortController } | null = null;
  const stop = signal.subscribe((path) => {
    if (path === fresh) {
      fresh = null;
      return;
    }
    fresh = null;
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
