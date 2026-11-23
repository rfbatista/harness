// The composition root of the browser: finds every [data-island] mount on the
// page, builds its adapters and presenters, and hydrates it (or renders it from
// scratch when the server render failed). An app-shell swap inserts new mounts
// and removes old ones: new ones are mounted, removed ones are unmounted.

import { createRoot, hydrateRoot } from "react-dom/client";
import { apiClient } from "../shared/infrastructure/api-client";
import { readIslandProps } from "../shared/infrastructure/island-props";
import { sharedFeed } from "../shared/infrastructure/shared-feed";
import { followNavigation } from "../features/navigation/application/follow-navigation";
import { browserNavigationSignal } from "../features/navigation/infrastructure/browser-navigation-signal";
import { browserPageOpener } from "../features/navigation/infrastructure/browser-page-opener";
import { httpTopBarSource } from "../features/navigation/infrastructure/http-top-bar-source";
import { browserNavigator } from "../features/notifications/infrastructure/browser-navigator";
import { httpNotificationsSource } from "../features/notifications/infrastructure/http-notifications-source";
import { httpContextSource } from "../features/context/infrastructure/http-context-source";
import { ContextPropsDto } from "../features/context/infrastructure/dto/context-props-dto";
import { ContextPresenter } from "../features/context/presentation/presenters/context-presenter";
import { ContextView } from "../features/context/presentation/views/context-view";
import { buildTopBar } from "./top-bar-presenters";
import { TopBarView } from "./top-bar-view";

type Dispose = () => void;

/** Mounts the island of `mount` with its `props`; returns what takes it down. */
/** `fresh`: a swap inserted the mount, with its own props, after the first page load. */
type Island = (mount: Element, props: unknown, fresh: boolean) => Dispose;

/** Mounts every island on the page and every one that a swap brings later. Returns what stops it all. */
export function mountIslands(doc: Document): Dispose {
  const win = doc.defaultView;
  if (!win) return () => {};
  const api = apiClient();
  // One shared page event stream for the document's islands (and, through it, every tab and the Alpine client).
  const feed = sharedFeed({ base: "/api", api });
  const topBarSource = httpTopBarSource(api);
  const notifications = httpNotificationsSource(api, feed);
  const contextSource = httpContextSource(api);

  const islands: Record<string, Island> = {
    "top-bar": (mount, props, fresh) => {
      const topBar = buildTopBar(props, { pageOpener: browserPageOpener(win.location), navigator: browserNavigator(win.location), notifications });
      const unfollow = followNavigation(browserNavigationSignal(win), topBarSource, topBar.current, { fresh });
      const node = <TopBarView presenters={topBar.presenters} />;
      // Hydrate what the server rendered; render from scratch when its render failed (or left nothing).
      const failed = mount.getAttribute("data-island-rendered") === "false" || !mount.hasChildNodes();
      const root = failed ? createRoot(mount) : hydrateRoot(mount, node);
      if (failed) root.render(node);
      return () => {
        unfollow();
        topBar.inbox?.stop();
        root.unmount();
      };
    },
    // The Context tab of the session pane: client-rendered, mounted while the tab is shown.
    "session-context": (mount, props) => {
      const presenter = new ContextPresenter(contextSource, ContextPropsDto.fromJSON(props).session_id);
      const root = createRoot(mount);
      root.render(<ContextView presenter={presenter} />);
      presenter.open();
      return () => root.unmount();
    },
  };

  const mounted = new Map<Element, Dispose>();

  function mount(el: Element, fresh: boolean): void {
    if (mounted.has(el) || !el.isConnected) return;
    const name = el.getAttribute("data-island") ?? "";
    const island = islands[name];
    if (!island) return;
    try {
      // The props script sits beside the mount, inside the same header.
      mounted.set(el, island(el, readIslandProps(el.parentElement ?? doc, name), fresh));
    } catch (err) {
      console.error(`island ${name} was not mounted`, err);
    }
  }

  /** Takes down the islands whose mounts a swap removed from the document. */
  function sweep(): void {
    for (const [el, dispose] of [...mounted]) {
      if (el.isConnected) continue;
      mounted.delete(el);
      dispose();
    }
  }

  const mountWithin = (root: ParentNode, fresh: boolean): void => {
    for (const el of root.querySelectorAll("[data-island]")) mount(el, fresh);
  };

  mountWithin(doc, false);
  const observer = new MutationObserver((records) => {
    sweep();
    for (const record of records) {
      for (const node of record.addedNodes) {
        if (!(node instanceof Element)) continue;
        if (node.hasAttribute("data-island")) mount(node, true);
        mountWithin(node, true);
      }
    }
  });
  observer.observe(doc, { childList: true, subtree: true });

  return () => {
    observer.disconnect();
    for (const dispose of mounted.values()) dispose();
    mounted.clear();
  };
}
