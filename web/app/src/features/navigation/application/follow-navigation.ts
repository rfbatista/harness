import type { CurrentTopBar } from "./current-top-bar";
import { loadTopBar } from "./load-top-bar";
import type { NavigationSignal, TopBarSource } from "./ports";

/**
 * Keeps the top bar on screen in step with the page: on every navigation it
 * reads the model for the new path. An answer that a later navigation
 * overtook is dropped, and a failed read keeps the last model. A path the top
 * bar already shows, or is already being asked for, is not asked for again
 * (a Back is both a popstate and an in-place navigation). Returns the unsubscribe.
 */
export function followNavigation(signal: NavigationSignal, source: TopBarSource, current: CurrentTopBar): () => void {
  let latest = current.get().path;
  let inFlight: AbortController | null = null;
  const stop = signal.subscribe((path) => {
    if (path === latest) return;
    latest = path;
    inFlight?.abort();
    const controller = new AbortController();
    inFlight = controller;
    loadTopBar(source, path, controller.signal).then(
      (topBar) => {
        if (inFlight === controller) current.set(topBar);
      },
      () => {
        // Keep the last model, and let the person's next move ask again.
        if (inFlight === controller) latest = current.get().path;
      },
    );
  });
  return () => {
    stop();
    inFlight?.abort();
  };
}
