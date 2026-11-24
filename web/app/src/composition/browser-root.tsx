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
import { OwnerDock } from "../features/product-owner/application/owner-dock";
import { browserDockMemory, browserPageNavigator, browserScreen, browserStorage } from "../features/product-owner/infrastructure/browser-dock";
import { httpProductOwner } from "../features/product-owner/infrastructure/http-product-owner";
import { ThreadList } from "../features/product-owner/application/thread-list";
import { AboutPresenter } from "../features/product-owner/presentation/presenters/about-presenter";
import { ThreadListPresenter } from "../features/product-owner/presentation/presenters/thread-list-presenter";
import { systemClock } from "../shared/kernel/clock";
import { OwnerPagePropsDto } from "./owner-page-props-dto";
import { OwnerPagePresenter } from "../features/product-owner/presentation/presenters/owner-page-presenter";
import { OwnerPageView } from "../features/product-owner/presentation/views/owner-page-view";
import { OwnerPanelPropsDto } from "./owner-panel-props-dto";
import { Conversation } from "../features/product-owner/application/conversation";
import { browserTaskCatalog } from "../features/product-owner/infrastructure/browser-task-catalog";
import { ThreadPresenter } from "../features/product-owner/presentation/presenters/thread-presenter";
import { PanelPresenter } from "../features/product-owner/presentation/presenters/panel-presenter";
import { PanelView } from "../features/product-owner/presentation/views/panel-view";
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
  const owner = httpProductOwner(api, feed);
  // The product owner's dock is the document's: the toggle in the top bar and the panel beside #main share it, and it outlives a swap.
  const navigator = browserPageNavigator(win);
  const dock = new OwnerDock({ gateway: owner, events: owner, memory: browserDockMemory(browserStorage(win)), screen: browserScreen(win), navigator });

  const islands: Record<string, Island> = {
    "top-bar": (mount, props, fresh) => {
      const topBar = buildTopBar(props, { pageOpener: browserPageOpener(win.location), navigator: browserNavigator(win.location), notifications, dock });
      const unfollow = followNavigation(browserNavigationSignal(win), topBarSource, topBar.current, { fresh });
      const node = <TopBarView presenters={topBar.presenters} />;
      // Hydrate what the server rendered; render from scratch when its render failed (or left nothing).
      const failed = mount.getAttribute("data-island-rendered") === "false" || !mount.hasChildNodes();
      const root = failed ? createRoot(mount) : hydrateRoot(mount, node);
      if (failed) root.render(node);
      return () => {
        unfollow();
        topBar.inbox?.stop();
        topBar.release();
        root.unmount();
      };
    },
    // The panel: a client-rendered island beside #main, on every project page.
    "owner-panel": (mount, props) => {
      const { project_name, no_tasks } = OwnerPanelPropsDto.fromJSON(props);
      const conversation = new Conversation({ gateway: owner, events: owner, onThread: (id) => dock.setThreadId(id) });
      const thread = new ThreadPresenter(conversation, { projectName: project_name, noTasks: no_tasks }, browserTaskCatalog(doc));
      const list = new ThreadList({ gateway: owner, events: owner });
      const panel = new PanelPresenter(dock, conversation, thread, { list, threads: new ThreadListPresenter(list, conversation, systemClock), about: new AboutPresenter(conversation, list) });
      const root = createRoot(mount);
      root.render(<PanelView presenter={panel} />);
      return () => {
        panel.destroy();
        root.unmount();
      };
    },
    // The full page: the same thread component beside the thread list. The panel steps aside while it is open.
    "owner-page": (mount, props) => {
      const { project_id, project_name, thread_id } = OwnerPagePropsDto.fromJSON(props);
      const noTasks = browserTaskCatalog(doc).count() === 0;
      const conversation = new Conversation({ gateway: owner, events: owner });
      const list = new ThreadList({ gateway: owner, events: owner });
      const thread = new ThreadPresenter(conversation, { projectName: project_name, noTasks }, browserTaskCatalog(doc), dock.state.enabled === false);
      const threads = new ThreadListPresenter(list, conversation, systemClock);
      const about = new AboutPresenter(conversation, list);
      const page = new OwnerPagePresenter({ projectId: project_id, projectName: project_name, threadId: thread_id }, dock, conversation, list, navigator);
      // The product owner is switched off or on: the composer follows.
      const stopDock = dock.subscribe((state) => thread.setOff(state.enabled === false));
      const stopList = list.attach(project_id);
      const stopConversation = conversation.attach(project_id, thread_id === "" ? null : thread_id);
      const root = createRoot(mount);
      root.render(<OwnerPageView page={page} thread={thread} threads={threads} about={about} />);
      return () => {
        root.unmount();
        stopConversation();
        stopList();
        stopDock();
        threads.destroy();
        about.destroy();
        page.destroy();
      };
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
