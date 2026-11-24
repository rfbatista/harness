import { useEffect, useRef, useState } from "react";
import { usePresenter } from "../../../../shared/presentation/use-presenter";
import { pageHref as pageHrefOf } from "../../application/links";
import { MAX_WIDTH, MIN_WIDTH } from "../../application/owner-dock";
import type { PanelPresenter } from "../presenters/panel-presenter";
import { AboutPopupView } from "./about-popup-view";
import { ThreadView } from "./thread-view";
import { ThreadsPopupView } from "./threads-popup-view";
import { OwnerIcon, OwnerMark } from "./owner-icon";

/** The docked panel. It reaches out of its island once: it gives the app shell the column it sits in. */
export function PanelView({ presenter }: { presenter: PanelPresenter }) {
  const vm = usePresenter(presenter);
  const aside = useRef<HTMLElement>(null);
  const composer = useRef<HTMLTextAreaElement>(null);
  const seenFocus = useRef(vm.focusRequest);
  const about = usePresenter(presenter.about);
  const [popup, setPopup] = useState<"threads" | "about" | null>(null);
  const threadsButton = useRef<HTMLButtonElement>(null);
  const aboutButton = useRef<HTMLButtonElement>(null);
  const closePopup = () => setPopup(null);

  // The shell gets its third column while the panel is docked, and gives it back when it goes.
  useEffect(() => {
    if (!vm.docked) return;
    const shell = aside.current?.closest<HTMLElement>(".app-shell");
    shell?.setAttribute("data-owner", "");
    return () => {
      shell?.removeAttribute("data-owner");
    };
  }, [vm.docked]);
  useEffect(() => {
    if (!vm.docked) return;
    const shell = aside.current?.closest<HTMLElement>(".app-shell");
    shell?.style.setProperty("--size-owner", `${vm.width}rem`);
    return () => {
      shell?.style.removeProperty("--size-owner");
    };
  }, [vm.docked, vm.width]);

  // The panel opened: the composer takes focus.
  useEffect(() => {
    if (!vm.docked) return;
    if (vm.focusRequest !== seenFocus.current) composer.current?.focus();
    seenFocus.current = vm.focusRequest;
  }, [vm.docked, vm.focusRequest]);

  if (!vm.docked) return null;
  return (
    <aside ref={aside} className="[ owner-panel ]" aria-label="Product owner" onKeyDown={(event) => void (presenter.handleKey(event) && event.stopPropagation())}>
      <Resizer presenter={presenter} width={vm.width} aside={aside} />
      <header className="[ owner-head ]">
        <div className="[ who ]">
          <OwnerMark />
          <h2 className="[ name ]" style={{ fontSize: "var(--text-base)" }}>
            Product owner
          </h2>
          {about.title !== "" && (
            <span className="[ thread ]" title={about.title}>
              {about.title}
            </span>
          )}
        </div>
        <div className="[ actions ]">
          <button type="button" className="[ button ]" data-variant="ghost" data-size="sm" data-icon="" aria-label="New thread" title="New thread" onClick={presenter.threads.newThread}>
            <OwnerIcon name="plus" />
          </button>
          <button ref={threadsButton} type="button" className="[ button ]" data-variant="ghost" data-size="sm" data-icon="" aria-label="Threads" title="Threads" aria-haspopup="listbox" aria-expanded={popup === "threads"} onClick={() => setPopup(popup === "threads" ? null : "threads")}>
            <OwnerIcon name="history" />
          </button>
          <button ref={aboutButton} type="button" className="[ button ]" data-variant="ghost" data-size="sm" data-icon="" aria-label="About this thread" title="About this thread" aria-haspopup="dialog" aria-expanded={popup === "about"} onClick={() => setPopup(popup === "about" ? null : "about")}>
            <OwnerIcon name="info" />
          </button>
          <button type="button" className="[ button ]" data-variant="ghost" data-size="sm" data-icon="" aria-label="Open as a page" title="Open as a page" onClick={presenter.openAsPage}>
            <OwnerIcon name="expand" />
          </button>
          <button type="button" className="[ button ]" data-variant="ghost" data-size="sm" data-icon="" aria-label="Close" title="Close" onClick={presenter.close}>
            <OwnerIcon name="close" />
          </button>
        </div>
      </header>
      {vm.state === "off" && (
        <div className="[ banner ]" data-tone="attention" role="status" style={{ margin: "var(--space-m)" }}>
          <span>
            <strong>The product owner is off.</strong> Nobody can message it and no digest is recorded. Threads and memory are kept.
            {vm.error !== "" && <span role="alert"> {vm.error}</span>}
          </span>
          <button type="button" className="[ button ]" data-variant="primary" data-size="sm" onClick={() => void presenter.turnOn()}>
            Turn it on
          </button>
          <a className="[ button ]" data-size="sm" href={vm.settingsHref}>
            Settings
          </a>
        </div>
      )}
      <ThreadView presenter={presenter.thread} textarea={composer} />
      <ThreadsPopupView presenter={presenter.threads} open={popup === "threads"} anchor={threadsButton} onClose={closePopup} allHref={pageHrefOf(vm.projectId)} />
      <AboutPopupView presenter={presenter.about} open={popup === "about"} anchor={aboutButton} onClose={closePopup} />
    </aside>
  );
}

/** The drag handle on the panel's start edge: pointer drag and the arrow keys resize it. */
function Resizer({ presenter, width, aside }: { presenter: PanelPresenter; width: number; aside: { current: HTMLElement | null } }) {
  const stop = useRef<(() => void) | null>(null);
  // A drag in progress ends with the panel.
  useEffect(() => () => stop.current?.(), []);

  const startDrag = (event: React.PointerEvent<HTMLElement>) => {
    event.preventDefault();
    const doc = event.currentTarget.ownerDocument;
    const win = doc.defaultView;
    if (!win) return;
    const right = aside.current?.getBoundingClientRect().right ?? win.innerWidth;
    const rem = parseFloat(win.getComputedStyle(doc.documentElement).fontSize) || 16;
    const move = (e: PointerEvent) => presenter.resize((right - e.clientX) / rem);
    const end = () => {
      win.removeEventListener("pointermove", move);
      win.removeEventListener("pointerup", end);
      win.removeEventListener("pointercancel", end);
      stop.current = null;
    };
    win.addEventListener("pointermove", move);
    win.addEventListener("pointerup", end);
    win.addEventListener("pointercancel", end);
    stop.current = end;
  };

  return (
    <div
      className="[ resizer ]"
      role="separator"
      aria-orientation="vertical"
      aria-label="Resize the product owner panel"
      aria-valuemin={MIN_WIDTH}
      aria-valuemax={MAX_WIDTH}
      aria-valuenow={Math.round(width)}
      tabIndex={0}
      onPointerDown={startDrag}
      onKeyDown={(event) => void (presenter.handleResizerKey(event) && event.preventDefault())}
    />
  );
}
