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
import { OwnerSettingsPropsDto } from "./owner-settings-props-dto";
import { OwnerSwitch } from "../features/product-owner/application/owner-switch";
import { SettingsSectionPresenter } from "../features/product-owner/presentation/presenters/settings-section-presenter";
import { SettingsSectionView } from "../features/product-owner/presentation/views/settings-section-view";
import { OwnerPagePropsDto } from "./owner-page-props-dto";
import { OwnerPagePresenter } from "../features/product-owner/presentation/presenters/owner-page-presenter";
import { OwnerPageView } from "../features/product-owner/presentation/views/owner-page-view";
import { OwnerPanelPropsDto } from "./owner-panel-props-dto";
import { DigestTime } from "../features/product-owner/application/digest-time";
import { DigestTimePresenter } from "../features/product-owner/presentation/presenters/digest-time-presenter";
import { DigestTimeView } from "../features/product-owner/presentation/views/digest-time-view";
import { ProjectDigests } from "../features/product-owner/application/project-digests";
import { DigestsTabPresenter } from "../features/product-owner/presentation/presenters/digests-tab-presenter";
import { memoryAddress } from "../features/product-owner/application/links";
import { ProjectMemory } from "../features/product-owner/application/project-memory";
import { MemoryTabPresenter } from "../features/product-owner/presentation/presenters/memory-tab-presenter";
import { Conversation } from "../features/product-owner/application/conversation";
import { browserTaskCatalog } from "../features/product-owner/infrastructure/browser-task-catalog";
import { ThreadPresenter } from "../features/product-owner/presentation/presenters/thread-presenter";
import { PanelPresenter } from "../features/product-owner/presentation/presenters/panel-presenter";
import { PanelView } from "../features/product-owner/presentation/views/panel-view";
import { browserClipboard } from "../features/context/infrastructure/browser-clipboard";
import { httpContextSource } from "../features/context/infrastructure/http-context-source";
import { ContextPropsDto } from "../features/context/infrastructure/dto/context-props-dto";
import { browserTabFront } from "../features/context/infrastructure/browser-tab-front";
import { httpContextFeed } from "../features/context/infrastructure/http-context-feed";
import { MemoryPropsDto } from "../features/session-memory/infrastructure/dto/memory-props-dto";
import { feedMemoryStream } from "../features/session-memory/infrastructure/feed-memory-stream";
import { httpMemorySource } from "../features/session-memory/infrastructure/http-memory-source";
import { ChipPresenter } from "../features/session-memory/presentation/presenters/chip-presenter";
import { ChipView } from "../features/session-memory/presentation/views/chip-view";
import { ContextDotPresenter } from "../features/context/presentation/presenters/context-dot-presenter";
import { ContextDotView } from "../features/context/presentation/views/context-dot-view";
import { ContextPresenter } from "../features/context/presentation/presenters/context-presenter";
import { isGroupKey } from "../features/context/presentation/presenters/context-view-model";
import { ContextView } from "../features/context/presentation/views/context-view";
import { browserFeaturePrefs, cookieRailPrefs, timeoutScheduler, windowAnnouncer, windowPageOpener, windowRailOpener } from "../features/rail/infrastructure/browser-rail";
import { httpRailFeed } from "../features/rail/infrastructure/http-rail-feed";
import { httpServicesCount } from "../features/rail/infrastructure/http-services-count";
import { httpRailSource } from "../features/rail/infrastructure/http-rail-source";
import type { RailPresenter } from "../features/rail/presentation/presenters/rail-presenter";
import { TasksTogglePresenter } from "../features/rail/presentation/presenters/tasks-toggle-presenter";
import { RailView } from "../features/rail/presentation/views/rail-view";
import { TasksToggleView } from "../features/rail/presentation/views/tasks-toggle-view";
import { TasksToggleDto } from "./rail-props-dto";
import { buildRail } from "./rail-presenters";
import { buildTopBar } from "./top-bar-presenters";
import { TopBarView } from "./top-bar-view";
import { mountAgentsView, mountWhiteboard } from "./whiteboard-island";
import { mountTaskServices } from "./task-services-island";

type Dispose = () => void;

/** `#digests` or `#digests/<date>`: the Digests tab, at a date when the address names one. */
function digestsAddress(hash: string): { tab: "digests"; date: string } | undefined {
  const match = /^#digests(?:\/(\d{4}-\d{2}-\d{2}))?$/.exec(hash);
  return match ? { tab: "digests", date: match[1] ?? "" } : undefined;
}

/** Islands that read their props themselves, from the page they sit in. */
const OWN_PROPS = new Set(["agents-view", "agents-whiteboard", "task-services"]);

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
  const contextSource = httpContextSource(api);
  const contextFeed = httpContextFeed(feed);

  const railSource = httpRailSource(api);
  const railFeed = httpRailFeed(feed);
  const servicesCount = httpServicesCount(api, feed);
  // The header's Tasks button and the rail are two islands that share the rail's state: the button follows the rail once it is mounted.
  let currentRail: RailPresenter | null = null;
  const railWatchers = new Set<(rail: RailPresenter | null) => void>();
  const shareRail = (rail: RailPresenter | null): void => {
    currentRail = rail;
    for (const watch of [...railWatchers]) watch(rail);
  };

  const islands: Record<string, Island> = {
    // The rail beside <main>: the mount is the rail's own element (.rail), the server drew it and the browser takes over.
    rail: (mount, props) => {
      const features = browserFeaturePrefs(win);
      const rail = buildRail(props, { source: railSource, feed: railFeed, prefs: cookieRailPrefs(doc), features, announcer: windowAnnouncer(win), opener: windowPageOpener(win.location), scheduler: timeoutScheduler(win), services: servicesCount });
      // In-place navigation keeps the rail in the document: it follows the page and the task that are open (the frame state the swap wrote).
      const follow = () => {
        const state = doc.getElementById("frame-state");
        if (!state) return;
        rail.presenter.setPage(state.dataset["page"] ?? "");
        rail.presenter.setCurrentTask(state.dataset["task"] ?? "");
      };
      win.addEventListener("harness:navigated", follow);
      const node = <RailView presenter={rail.presenter} host={mount as HTMLElement} />;
      const failed = mount.getAttribute("data-island-rendered") === "false" || !mount.hasChildNodes();
      const root = failed ? createRoot(mount) : hydrateRoot(mount, node);
      if (failed) root.render(node);
      shareRail(rail.presenter);
      return () => {
        shareRail(null);
        win.removeEventListener("harness:navigated", follow);
        rail.stop();
        root.unmount();
      };
    },
    // The header's Tasks button: the mount is the link itself; it opens the rail's drawer and counts what needs the person.
    "tasks-toggle": (mount, props) => {
      const toggle = new TasksTogglePresenter(TasksToggleDto.fromJSON(props).needs_you);
      const follow = (rail: RailPresenter | null): void => {
        if (rail) toggle.follow(rail);
        else toggle.stop();
      };
      railWatchers.add(follow);
      if (currentRail) follow(currentRail);
      const node = <TasksToggleView presenter={toggle} host={mount as HTMLElement} onOpen={windowRailOpener(win)} />;
      const failed = mount.getAttribute("data-island-rendered") === "false" || !mount.hasChildNodes();
      const root = failed ? createRoot(mount) : hydrateRoot(mount, node);
      if (failed) root.render(node);
      return () => {
        railWatchers.delete(follow);
        toggle.stop();
        root.unmount();
      };
    },
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
      const memory = new ProjectMemory({ gateway: owner, events: owner });
      const thread = new ThreadPresenter(conversation, { projectName: project_name, noTasks: no_tasks }, browserTaskCatalog(doc), false, { memory });
      const list = new ThreadList({ gateway: owner, events: owner });
      const panel = new PanelPresenter(dock, conversation, thread, { memory, list, threads: new ThreadListPresenter(list, conversation, systemClock), about: new AboutPresenter(conversation, list) });
      const root = createRoot(mount);
      root.render(<PanelView presenter={panel} />);
      return () => {
        panel.destroy();
        root.unmount();
      };
    },
    // The Settings page's Product owner section: the switch.
    "owner-settings": (mount, props) => {
      const { project_id } = OwnerSettingsPropsDto.fromJSON(props);
      const ownerSwitch = new OwnerSwitch({ gateway: owner, events: owner });
      const section = new SettingsSectionPresenter(ownerSwitch, project_id);
      const stop = ownerSwitch.attach(project_id);
      const digestTime = new DigestTime({ gateway: owner, events: owner });
      const digest = new DigestTimePresenter(digestTime, project_id);
      const stopDigestTime = digestTime.attach(project_id);
      const root = createRoot(mount);
      root.render(
        <div className="[ flow ]">
          <SettingsSectionView presenter={section} />
          <DigestTimeView presenter={digest} />
        </div>,
      );
      return () => {
        root.unmount();
        stop();
        stopDigestTime();
        digest.destroy();
        section.destroy();
      };
    },
    // The full page: the same thread component beside the thread list. The panel steps aside while it is open.
    "owner-page": (mount, props) => {
      const { project_id, project_name, thread_id } = OwnerPagePropsDto.fromJSON(props);
      const noTasks = browserTaskCatalog(doc).count() === 0;
      const conversation = new Conversation({ gateway: owner, events: owner });
      const list = new ThreadList({ gateway: owner, events: owner });
      const memory = new ProjectMemory({ gateway: owner, events: owner });
      const opened = digestsAddress(win.location.hash) ?? memoryAddress(win.location.hash);
      const initial = opened?.tab === "memory" ? opened : undefined;
      const page = new OwnerPagePresenter({ projectId: project_id, projectName: project_name, threadId: thread_id, initial: opened?.tab === "digests" ? { tab: "digests" } : initial }, dock, conversation, list, navigator, memory);
      const thread = new ThreadPresenter(conversation, { projectName: project_name, noTasks }, browserTaskCatalog(doc), dock.state.enabled === false, { memory, openEntry: (entryId) => {
        page.showMemory(entryId);
        memoryTab.showEntry(entryId);
      } });
      const memoryTab = new MemoryTabPresenter(memory, project_id, initial?.entryId);
      const threads = new ThreadListPresenter(list, conversation, systemClock);
      const about = new AboutPresenter(conversation, list);
      // The product owner is switched off or on: the composer follows.
      const stopDock = dock.subscribe((state) => thread.setOff(state.enabled === false));
      const stopList = list.attach(project_id);
      const stopMemory = memory.attach(project_id);
      const projectDigests = new ProjectDigests({ gateway: owner, events: owner });
      const digests = new DigestsTabPresenter(projectDigests, project_id, "", (date) => page.showDigests(date));
      const stopDigests = projectDigests.attach(project_id);
      // The address names the Digests tab and its date: where the page opens, and where Back, Forward and an edit of the address take it.
      const followAddress = () => {
        // Each tab switch writes its own address, and each address opens its own tab.
        const address = digestsAddress(win.location.hash);
        const memoryAt = memoryAddress(win.location.hash);
        if (address !== undefined) {
          page.showDigests(address.date);
          void digests.show(address.date);
        } else if (memoryAt !== undefined) {
          page.showMemory(memoryAt.entryId);
          memoryTab.showEntry(memoryAt.entryId);
        } else if (page.viewModel.tab !== "threads") page.showThreads();
      };
      if (opened?.tab === "digests") void digests.show(opened.date);
      win.addEventListener("hashchange", followAddress);
      const stopConversation = conversation.attach(project_id, thread_id === "" ? null : thread_id);
      const root = createRoot(mount);
      root.render(<OwnerPageView page={page} thread={thread} threads={threads} about={about} memory={memoryTab} digests={digests} />);
      return () => {
        root.unmount();
        stopMemory();
        memoryTab.destroy();
        stopConversation();
        stopList();
        win.removeEventListener("hashchange", followAddress);
        stopDigests();
        digests.destroy();
        stopDock();
        threads.destroy();
        about.destroy();
        page.destroy();
      };
    },
    // The Context tab of the session pane: client-rendered, mounted while the tab is shown.
    "session-context": (mount, props) => {
      const { session_id, project_id, live, group, parent, focus, whiteboard_href } = ContextPropsDto.fromJSON(props);
      const presenter = new ContextPresenter(contextSource, session_id, { clipboard: browserClipboard(win.navigator), feed: contextFeed, projectId: project_id, live, parent, whiteboardHref: whiteboard_href === "" ? undefined : whiteboard_href });
      const root = createRoot(mount);
      root.render(<ContextView presenter={presenter} />);
      if (isGroupKey(group)) presenter.openAt(group);
      if (focus) presenter.focusSearch();
      presenter.open();
      return () => {
        presenter.close();
        root.unmount();
      };
    },
    // The dot inside the Context tab's button: watches the session while another tab is in front.
    "session-context-dot": (mount, props) => {
      const { session_id, project_id, live } = ContextPropsDto.fromJSON(props);
      const presenter = new ContextDotPresenter(contextFeed, browserTabFront(mount), { sessionId: session_id, projectId: project_id, live });
      const root = createRoot(mount);
      root.render(<ContextDotView presenter={presenter} />);
      return () => {
        presenter.close();
        root.unmount();
      };
    },
    // The memory chip in the session head, beside the token total: one island per selected session.
    "session-memory": (mount, props) => {
      const { session_id, live } = MemoryPropsDto.fromJSON(props);
      const presenter = new ChipPresenter({ source: httpMemorySource(api), stream: feedMemoryStream(feed), clock: systemClock, sessionId: session_id, live, scheduler: { after: (ms, fn) => { const id = win.setTimeout(fn, ms); return () => win.clearTimeout(id); } }, time: (at) => at.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" }) });
      const root = createRoot(mount);
      root.render(<ChipView presenter={presenter} toastHost={win.document.body} />);
      void presenter.start();
      return () => {
        presenter.dispose();
        root.unmount();
      };
    },
    "agents-view": () => mountAgentsView(win),
    "agents-whiteboard": (mount) => mountWhiteboard(mount, win, api, feed),
    "task-services": (mount) => mountTaskServices(mount, win, api, feed),
  };

  const mounted = new Map<Element, Dispose>();

  function mount(el: Element, fresh: boolean): void {
    if (mounted.has(el) || !el.isConnected) return;
    const name = el.getAttribute("data-island") ?? "";
    const island = islands[name];
    if (!island) return;
    try {
      // The props script sits beside the mount, inside the same header; the agents whiteboard islands read the task page's own seed.
      const props = OWN_PROPS.has(name) ? undefined : readIslandProps(el.parentElement ?? doc, name);
      mounted.set(el, island(el, props, fresh));
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
