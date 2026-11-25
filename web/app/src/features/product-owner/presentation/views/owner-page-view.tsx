import { useRef, useState } from "react";
import { usePresenter } from "../../../../shared/presentation/use-presenter";
import type { DigestsTabPresenter } from "../presenters/digests-tab-presenter";
import type { MemoryTabPresenter } from "../presenters/memory-tab-presenter";
import type { AboutPresenter } from "../presenters/about-presenter";
import type { OwnerPagePresenter } from "../presenters/owner-page-presenter";
import type { ThreadListPresenter } from "../presenters/thread-list-presenter";
import type { ThreadPresenter } from "../presenters/thread-presenter";
import { DigestsTabView } from "./digests-tab-view";
import { AboutPopupView } from "./about-popup-view";
import { MemoryTabView } from "./memory-tab-view";
import { OwnerIcon, OwnerMark } from "./owner-icon";
import { ThreadView } from "./thread-view";
import { ThreadListView, ThreadsPopupView } from "./threads-popup-view";

export interface OwnerPageParts {
  readonly page: OwnerPagePresenter;
  readonly thread: ThreadPresenter;
  readonly threads: ThreadListPresenter;
  readonly about: AboutPresenter;
  readonly memory: MemoryTabPresenter;
  readonly digests: DigestsTabPresenter;
}

/** The full page: toolbar, tabs, the thread column beside the conversation. Its parts are the grid items of <main class="owner-page">. */
export function OwnerPageView({ page, thread, threads, about, memory, digests }: OwnerPageParts) {
  const vm = usePresenter(page);
  const aboutVm = usePresenter(about);
  const composer = useRef<HTMLTextAreaElement>(null);
  const [popup, setPopup] = useState<"threads" | "about" | null>(null);
  const threadsButton = useRef<HTMLButtonElement>(null);
  const aboutButton = useRef<HTMLButtonElement>(null);
  const closePopup = () => setPopup(null);

  return (
    <>
      <header className="[ toolbar ]">
        <a className="[ crumb ]" href={vm.crumbHref}>
          {vm.projectName} /
        </a>
        <h1>
          <OwnerMark />
          Product owner
        </h1>
        <div className="[ actions ]">
          <button type="button" className="[ button ]" data-size="sm" onClick={page.newThread}>
            <OwnerIcon name="plus" />
            New thread
          </button>
          <a className="[ button ]" data-size="sm" href={vm.settingsHref}>
            Settings
          </a>
        </div>
      </header>
      <div className="[ tabs ]" role="tablist" aria-label="Product owner">
        <a
          role="tab"
          aria-selected={vm.tab === "threads"}
          href={vm.crumbHref + "/product-owner"}
          onClick={(event) => {
            event.preventDefault();
            page.showThreads();
          }}
        >
          Threads {vm.threadCount !== null && <span className="[ badge ]">{vm.threadCount}</span>}
        </a>
        <a
          role="tab"
          aria-selected={vm.tab === "memory"}
          href={vm.crumbHref + "/product-owner#memory"}
          onClick={(event) => {
            event.preventDefault();
            page.showMemory();
          }}
        >
          Memory {vm.memoryCount !== null && <span className="[ badge ]">{vm.memoryCount}</span>}
        </a>
        <a
          role="tab"
          aria-selected={vm.tab === "digests"}
          href={vm.crumbHref + "/product-owner#digests"}
          onClick={(event) => {
            event.preventDefault();
            page.showDigests();
          }}
        >
          Digests
        </a>
      </div>
      {vm.tab === "memory" && <MemoryTabView presenter={memory} onOpenThread={page.openThread} onStartThread={page.newThread} />}
      {vm.tab === "digests" && <DigestsTabView presenter={digests} />}
      <div className="[ owner-split ]" data-phone={vm.phone ?? undefined} hidden={vm.tab !== "threads"}>
        <ThreadListView presenter={threads} onOpen={page.openThread} onNew={page.newThread} />
        <section className="[ conversation ]" aria-label={aboutVm.title === "" ? "New thread" : aboutVm.title}>
          <header className="[ owner-head ]" data-page="">
            <div className="[ who ]">
              {vm.phone === "thread" && (
                <button type="button" className="[ button ]" data-variant="ghost" data-size="sm" data-icon="" aria-label="Back to threads" title="Back to threads" onClick={page.back}>
                  <OwnerIcon name="back" />
                </button>
              )}
              <h2 className="[ name ]" style={{ fontSize: "var(--text-base)" }}>
                {aboutVm.title === "" ? "New thread" : aboutVm.title}
              </h2>
            </div>
            <div className="[ actions ]">
              <button ref={threadsButton} type="button" className="[ button ] [ threads-button ]" data-variant="ghost" data-size="sm" data-icon="" aria-label="Threads" title="Threads" aria-haspopup="listbox" aria-expanded={popup === "threads"} onClick={() => setPopup(popup === "threads" ? null : "threads")}>
                <OwnerIcon name="history" />
              </button>
              <button ref={aboutButton} type="button" className="[ button ]" data-variant="ghost" data-size="sm" data-icon="" aria-label="About this thread" title="About this thread" aria-haspopup="dialog" aria-expanded={popup === "about"} onClick={() => setPopup(popup === "about" ? null : "about")}>
                <OwnerIcon name="info" />
              </button>
              <button type="button" className="[ button ]" data-variant="ghost" data-size="sm" data-icon="" aria-label="Dock beside the page" title="Dock beside the page" onClick={page.dockBeside}>
                <OwnerIcon name="collapse" />
              </button>
            </div>
          </header>
          {vm.notFound && (
            <div className="[ banner ]" data-tone="attention" role="alert" style={{ margin: "var(--space-m)" }}>
              <span>That thread is not a thread of this project. You can start a new one.</span>
            </div>
          )}
          <ThreadView presenter={thread} textarea={composer} />
        </section>
        <ThreadsPopupView presenter={threads} open={popup === "threads"} anchor={threadsButton} onClose={closePopup} allHref={vm.crumbHref + "/product-owner"} />
        <AboutPopupView presenter={about} open={popup === "about"} anchor={aboutButton} onClose={closePopup} />
      </div>
    </>
  );
}
