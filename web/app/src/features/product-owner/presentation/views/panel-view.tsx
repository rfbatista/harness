import { useEffect, useRef } from "react";
import { usePresenter } from "../../../../shared/presentation/use-presenter";
import { MAX_WIDTH, MIN_WIDTH } from "../../application/owner-dock";
import type { PanelPresenter } from "../presenters/panel-presenter";
import { ComposerView } from "./composer-view";
import { OwnerIcon, OwnerMark } from "./owner-icon";

/** The docked panel. It reaches out of its island once: it gives the app shell the column it sits in. */
export function PanelView({ presenter }: { presenter: PanelPresenter }) {
  const vm = usePresenter(presenter);
  const aside = useRef<HTMLElement>(null);
  const composer = useRef<HTMLTextAreaElement>(null);
  const seenFocus = useRef(vm.focusRequest);

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
        </div>
        <div className="[ actions ]">
          <button type="button" className="[ button ]" data-variant="ghost" data-size="sm" data-icon="" aria-label="Close" title="Close" onClick={presenter.close}>
            <OwnerIcon name="close" />
          </button>
        </div>
      </header>
      {vm.state === "off" ? (
        <div className="[ thread ]">
          <div className="[ thread-empty ]">
            <h2>The product owner is off</h2>
            <p>Nobody can message it and no digest is recorded. Threads and memory are kept.</p>
            {vm.error !== "" && (
              <p role="alert" style={{ color: "var(--color-danger)" }}>
                {vm.error}
              </p>
            )}
            <div className="[ cluster ] [ gutter-xs ]">
              <button type="button" className="[ button ]" data-variant="primary" data-size="sm" onClick={() => void presenter.turnOn()}>
                Turn it on
              </button>
              <a className="[ button ]" data-size="sm" href={vm.settingsHref}>
                Settings
              </a>
            </div>
          </div>
        </div>
      ) : (
        <div className="[ thread ]" role="log" aria-live="off" />
      )}
      <ComposerView textarea={composer} placeholder={vm.composerPlaceholder} disabled={vm.composerDisabled} />
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
