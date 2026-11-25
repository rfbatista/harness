// The full page: /projects/{project}/product-owner, with the thread list beside the
// conversation. It shows the same thread component as the panel. While it is open the panel is
// hidden. Under 48 rem it shows the thread list first, and a thread in place of it with Back.

import { Presenter } from "../../../../shared/kernel/presenter";
import type { Conversation } from "../../application/conversation";
import { pageHref, settingsHref, threadHref } from "../../application/links";
import type { OwnerDock } from "../../application/owner-dock";
import type { PageNavigator } from "../../application/ports";
import type { ThreadList } from "../../application/thread-list";

export interface OwnerPageViewModel {
  readonly projectName: string;
  readonly crumbHref: string;
  readonly settingsHref: string;
  /** The Threads tab's count; null until the list is read. */
  readonly threadCount: number | null;
  /** Under 48 rem: which of the list and the thread shows; null on a wider screen. */
  readonly phone: "list" | "thread" | null;
  /** The thread the address names is not a thread of this project. */
  readonly notFound: boolean;
}

export interface OwnerPageProps {
  readonly projectId: string;
  readonly projectName: string;
  /** The thread the address names; "" on the page itself. */
  readonly threadId: string;
}

export class OwnerPagePresenter extends Presenter<OwnerPageViewModel> {
  readonly #props: OwnerPageProps;
  readonly #dock: OwnerDock;
  readonly #conversation: Conversation;
  readonly #list: ThreadList;
  readonly #navigator: PageNavigator;
  readonly #stops: Array<() => void> = [];
  #showList: boolean;
  #address: string;

  constructor(props: OwnerPageProps, dock: OwnerDock, conversation: Conversation, list: ThreadList, navigator: PageNavigator) {
    super({ projectName: props.projectName, crumbHref: "", settingsHref: "", threadCount: null, phone: null, notFound: false });
    this.#props = props;
    this.#dock = dock;
    this.#conversation = conversation;
    this.#list = list;
    this.#navigator = navigator;
    this.#showList = props.threadId === "";
    this.#address = props.threadId === "" ? pageHref(props.projectId) : threadHref(props.projectId, props.threadId);
    // The page is open: the panel steps aside, and everything of the page lets go when it is destroyed.
    this.#stops.push(dock.suspend());
    this.#stops.push(
      dock.subscribe(() => this.#rebuild()),
      list.subscribe(() => this.#rebuild()),
      conversation.subscribe((state) => {
        this.#follow(state.threadId);
        this.#rebuild();
      }),
    );
  }

  destroy(): void {
    for (const stop of this.#stops.splice(0)) stop();
  }

  /** A thread of the list: on a phone it opens in place of the list. */
  openThread = (threadId: string): void => {
    this.#showList = false;
    this.#conversation.openThread(threadId);
    this.#rebuild();
  };

  newThread = (): void => {
    this.#showList = false;
    this.#conversation.newThread();
    this.#rebuild();
  };

  /** Back, on a phone: from the thread to the list. */
  back = (): void => {
    this.#showList = true;
    this.#rebuild();
  };

  /** "Dock beside the page": the thread opens in the panel, beside the board. */
  dockBeside = (): void => {
    this.#dock.setThreadId(this.#conversation.state.threadId);
    this.#dock.dockBeside();
  };

  /** The thread on screen has its own address; the page keeps the address in step with it. */
  #follow(threadId: string | null): void {
    if (this.#conversation.state.loading && threadId === null) return;
    const address = threadId === null ? pageHref(this.#props.projectId) : threadHref(this.#props.projectId, threadId);
    if (address === this.#address) return;
    this.#address = address;
    this.#navigator.replace(address);
  }

  #rebuild(): void {
    const { projectId } = this.#props;
    this.set({
      projectName: this.#props.projectName,
      crumbHref: `/projects/${encodeURIComponent(projectId)}`,
      settingsHref: settingsHref(projectId),
      threadCount: this.#list.state.loading ? null : this.#list.state.threads.length,
      phone: this.#dock.phone ? (this.#showList ? "list" : "thread") : null,
      notFound: this.#conversation.state.notFound,
    });
  }
}
