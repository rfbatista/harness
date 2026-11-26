// The agents whiteboard, passive: it renders the presenter's view model and
// forwards the person's actions to it (ADR 0108). Classes come from the
// design system's whiteboard block.

import { useCallback, useRef } from "react";
import { usePresenter } from "../../../../shared/presentation/use-presenter";
import type { BoardPresenter } from "../presenters/board-presenter";
import { CardView } from "./card-view";

/** The stroke each edge tone takes from the design system block. */
const KIND = { started: "started", signal: "messages", amber: "review" } as const;

const ZOOM_STEP = 1.2;

export function BoardView({ presenter }: { presenter: BoardPresenter }) {
  const vm = usePresenter(presenter);
  const canvas = useRef<HTMLDivElement | null>(null);
  const pan = useRef<{ x: number; y: number } | null>(null);
  const { w: width, h: height } = vm.canvas;

  // The canvas size is the camera's viewport; the wheel is a non-passive listener so it can keep the page still.
  // A ref callback (not an effect): React calls it with the element when it mounts and runs what it returns when it unmounts.
  const attach = useCallback(
    (el: HTMLDivElement | null) => {
      if (!el) return;
      canvas.current = el;
      const measure = () => presenter.setViewport(el.clientWidth, el.clientHeight);
      measure();
      const observer = new ResizeObserver(measure);
      observer.observe(el);
      const wheel = (event: WheelEvent) => {
        event.preventDefault();
        const box = el.getBoundingClientRect();
        if (event.ctrlKey || event.metaKey) presenter.zoomAt(Math.exp(-event.deltaY * 0.01), event.clientX - box.left, event.clientY - box.top);
        else presenter.panBy(-event.deltaX, -event.deltaY);
      };
      el.addEventListener("wheel", wheel, { passive: false });
      return () => {
        observer.disconnect();
        el.removeEventListener("wheel", wheel);
      };
    },
    [presenter],
  );

  /** Keyboard focus follows the presenter's: the card it walked to, or the canvas after Escape. */
  const followFocus = () => {
    const el = canvas.current;
    if (!el) return;
    const id = presenter.viewModel.focusedId;
    const target = id === null ? el : [...el.querySelectorAll<HTMLElement>(".wb-node")].find((n) => n.dataset.id === id);
    target?.focus({ preventScroll: true });
  };

  const key = (event: React.KeyboardEvent) => {
    if (event.metaKey || event.ctrlKey || event.altKey) return;
    // Tools in the bar keep their own keys; the canvas and its cards are the board's.
    if ((event.target as HTMLElement).closest(".wb-canvas") && presenter.key({ key: event.key })) {
      event.preventDefault();
      followFocus();
      return;
    }
    const handled = { t: () => presenter.tidy(), "1": () => presenter.fit(), "0": () => presenter.actualSize(), "+": () => presenter.zoomBy(ZOOM_STEP), "=": () => presenter.zoomBy(ZOOM_STEP), "-": () => presenter.zoomBy(1 / ZOOM_STEP) }[event.key.toLowerCase()];
    if (!handled) return;
    event.preventDefault();
    handled();
  };

  return (
    <section className="whiteboard" aria-label="Agents whiteboard" data-layout="rank" onKeyDown={key}>
      <div className="wb-bar">
        {vm.memory && (
          <span className="wb-summary" title={vm.memory.sentence}>
            <strong>{vm.memory.live} live</strong>
            {vm.memory.total && (
              <>
                {" · memory "}
                <strong>{vm.memory.total}</strong>
              </>
            )}
            {vm.memory.near > 0 && (
              <span className="badge" data-tone="attention">
                {vm.memory.near} near budget
              </span>
            )}
            {vm.memory.over > 0 && (
              <span className="badge" data-tone="danger">
                {vm.memory.over} over budget
              </span>
            )}
            {vm.memory.ended > 0 && (
              <span className="badge" data-tone="danger">
                {vm.memory.ended} process ended
              </span>
            )}
          </span>
        )}
        {vm.arrival && (
          <span className="wb-live">
            {vm.arrival.text.replace(" · Show", " · ")}
            <button type="button" className="button" data-variant="ghost" data-size="sm" data-arrival-show onClick={() => presenter.showArrival()}>
              Show
            </button>
          </span>
        )}
        {vm.state === "ended" && <span className="wb-live">{vm.endedNote}</span>}
        {vm.feed === "paused" && <span className="wb-live">Reconnecting · the picture stays as it was</span>}
        {vm.feed === "resynced" && <span className="wb-live">Back online · updated</span>}
        <div className="wb-tools">
          <button type="button" className="button" data-variant="ghost" data-size="sm" data-tidy disabled={!vm.tidyEnabled} title="Tidy (T): put every card back in its computed place" onClick={() => presenter.tidy()}>
            Tidy
            {vm.placedCount > 0 && <span className="badge">{vm.placedCount} placed</span>}
          </button>
          <span className="sep" />
          <button type="button" className="button" data-variant="ghost" data-size="sm" data-fit title="Fit (1)" onClick={() => presenter.fit()}>
            Fit
          </button>
          <button type="button" className="button" data-variant="ghost" data-size="sm" data-icon aria-label="Zoom out (−)" onClick={() => presenter.zoomBy(1 / ZOOM_STEP)}>
            −
          </button>
          <span className="zoom" data-zoom>
            {vm.zoomPercent}%
          </span>
          <button type="button" className="button" data-variant="ghost" data-size="sm" data-icon aria-label="Zoom in (+)" onClick={() => presenter.zoomBy(ZOOM_STEP)}>
            +
          </button>
          <button type="button" className="button" data-variant="ghost" data-size="sm" data-zoom-reset title="Actual size (0)" onClick={() => presenter.actualSize()}>
            100%
          </button>
        </div>
      </div>
      <div
        ref={attach}
        className="wb-canvas"
        tabIndex={0}
        role="group"
        aria-roledescription="whiteboard"
        aria-label={vm.canvasLabel}
        onPointerDown={(event) => {
          if (event.button !== 0 || (event.target as HTMLElement).closest(".wb-node, .wb-pill")) return;
          pan.current = { x: event.clientX, y: event.clientY };
          event.currentTarget.setPointerCapture(event.pointerId);
          event.currentTarget.setAttribute("data-panning", "");
        }}
        onPointerMove={(event) => {
          const from = pan.current;
          if (!from) return;
          presenter.panBy(event.clientX - from.x, event.clientY - from.y);
          pan.current = { x: event.clientX, y: event.clientY };
        }}
        onPointerUp={(event) => {
          pan.current = null;
          event.currentTarget.removeAttribute("data-panning");
        }}
      >
        <div
          className="wb-world"
          role="list"
          data-compact={vm.compact ? "" : undefined}
          style={{ inlineSize: width, blockSize: height, transform: `translate(${vm.camera.x}px, ${vm.camera.y}px) scale(${vm.camera.scale})` }}
        >
          {vm.columns.map((column) => (
            <div key={column.key} className="wb-lane" aria-hidden="true" style={{ left: column.x, top: column.y, inlineSize: column.w }}>
              {column.label} <span className="count">{column.count}</span>
            </div>
          ))}
          <svg className="wb-edges" width={width} height={height} aria-hidden="true">
            {vm.edges.map((e) => (
              <path key={e.id} d={e.d} data-kind={KIND[e.tone]} data-dim={e.dimmed ? "" : undefined} data-pulse={e.pulsing ? "" : undefined} />
            ))}
          </svg>
          {vm.edges.map((e) =>
            e.badge ? (
              <button key={e.id} type="button" className="wb-pill" data-edge={e.id} onClick={() => presenter.openThread(e.id)} data-tone={e.badge.tone || undefined} style={{ left: e.badge.x, top: e.badge.y }} title={e.badge.name} aria-label={e.badge.name}>
                <span className="n">{e.badge.count}</span>
                <span className="latest">{e.badge.text}</span>
              </button>
            ) : null,
          )}
          {vm.cards.map((card) => (
            <CardView key={card.id} card={card} presenter={presenter} />
          ))}
        </div>
        {vm.state === "empty" && (
          <div className="wb-empty">
            <h2>No agents on this task yet</h2>
            <p>Start a session and it appears here. An architect you start draws its delegates under it as it spawns them.</p>
            <div className="actions">
              <button type="button" className="button" data-variant="primary" data-size="sm" onClick={() => presenter.newSession()}>
                New session
              </button>
              <button type="button" className="button" data-variant="ghost" data-size="sm" onClick={() => presenter.showList()}>
                Show the list
              </button>
            </div>
          </div>
        )}
      </div>
      {vm.memory?.total && (
        <div className="wb-legend">
          <span className="k">
            <span className="mem-key" /> memory of its budget
          </span>
        </div>
      )}
      <p className="visually-hidden" aria-live="polite" data-announce>
        {vm.announcement}
      </p>
    </section>
  );
}
