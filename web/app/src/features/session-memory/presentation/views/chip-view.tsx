import { useEffect, useRef, useState, type CSSProperties } from "react";
import { createPortal } from "react-dom";
import { usePopup } from "../../../../shared/presentation/use-popup";
import { usePresenter } from "../../../../shared/presentation/use-presenter";
import type { ChipPresenter, ToastView } from "../presenters/chip-presenter";

const POPOVER_ID = "memory-popover";

/** The memory chip of the session head, and the popover that hangs from it: nothing until the session has a sample. */
export function ChipView({ presenter, toastHost }: { presenter: ChipPresenter; toastHost: Element }) {
  const { view, popover, toasts, more, signal } = usePresenter(presenter);
  // The ring turns once per kill: it is dropped when its animation ends, and not set again for the same kill.
  const [rung, setRung] = useState<string | null>(null);
  const chip = useRef<HTMLButtonElement>(null);
  const panel = useRef<HTMLElement>(null);
  const open = popover !== null;
  usePopup(open, panel, chip, () => presenter.close());

  // Esc closes the popover first, then the toast that has the focus; the chip gets the focus back from the popover.
  const alive = open || toasts.length > 0;
  useEffect(() => {
    const doc = chip.current?.ownerDocument ?? toastHost.ownerDocument;
    if (!alive) return;
    const onKey = (event: KeyboardEvent) => {
      if (event.key !== "Escape") return;
      const wasOpen = presenter.viewModel.popover !== null;
      if (!presenter.escape()) return;
      event.stopPropagation();
      if (wasOpen) chip.current?.focus();
    };
    doc.addEventListener("keydown", onKey, true);
    return () => doc.removeEventListener("keydown", onKey, true);
  }, [alive, presenter, toastHost]);

  const region = createPortal(<ToastRegion presenter={presenter} toasts={toasts} more={more} />, toastHost);
  if (!view) return region;
  return (
    <>
      <button
        ref={chip}
        type="button"
        className="[ memory ]"
        data-state={view.state}
        data-signal={signal !== null && signal !== rung ? "" : undefined}
        aria-label={view.title}
        title={view.title}
        aria-expanded={open}
        aria-controls={POPOVER_ID}
        style={{ "--_fill": `${view.fillPercent}%` } as CSSProperties}
        onClick={() => void presenter.toggle()}
      >
        <span className="num">
          {view.num}
          <span className="of">{view.of}</span>
        </span>
        {view.word && <span className="word">{view.word}</span>}
        <span className="track" aria-hidden="true" onAnimationEnd={() => setRung(signal)}>
          <span className="fill" />
        </span>
      </button>
      <section ref={panel} id={POPOVER_ID} className="[ popup ] [ memory-popover ]" role="dialog" aria-label="Memory of this session">
        {popover && (
          <>
            <h3>{popover.title}</h3>
            <span className="bar" data-state={popover.state} style={{ "--_fill": `${popover.fillPercent}%` } as CSSProperties} aria-hidden="true">
              <span className="fill" />
            </span>
            <dl>
              <dt>Processes</dt>
              <dd>{popover.processes}</dd>
              <dt>Heaviest</dt>
              <dd>{popover.heaviest}</dd>
              <dt>Orçamento</dt>
              <dd>{popover.budget}</dd>
              {popover.prevention !== "" && (
                <>
                  <dt>Prevention</dt>
                  <dd>{popover.prevention}</dd>
                </>
              )}
            </dl>
            <h4>Ended this run</h4>
            {popover.kills.length === 0 ? (
              <p>{popover.killsNote}</p>
            ) : (
              <ul>
                {popover.kills.map((kill) => (
                  <li key={kill.key}>
                    <span>{kill.time}</span> <code>{kill.command}</code> <b>{kill.rss}</b> <span>{kill.signal}</span>
                  </li>
                ))}
              </ul>
            )}
            <p className="note">Over its orçamento, the session's heaviest process is ended. The agent itself never is.</p>
          </>
        )}
      </section>
      {region}
    </>
  );
}

function ToastRegion({ presenter, toasts, more }: { presenter: ChipPresenter; toasts: readonly ToastView[]; more: number }) {
  if (toasts.length === 0) return null;
  return (
    <div className="[ toast-region ] [ memory-toasts ]">
      {toasts.map((toast) => (
        <div
          key={toast.key}
          className="[ toast ]"
          data-tone="danger"
          role="alert"
          onMouseEnter={() => presenter.hold(toast.key, "hover")}
          onMouseLeave={() => presenter.release(toast.key, "hover")}
          onFocus={() => presenter.hold(toast.key, "focus")}
          onBlur={(event) => {
            if (!event.currentTarget.contains(event.relatedTarget)) presenter.release(toast.key, "focus");
          }}
        >
          <span className="[ status ]" data-state="failed" data-dot-only />
          <div className="body">
            <strong>{toast.title}</strong>
            <p className="meta">
              It had <b>{toast.rss}</b>
              {toast.notExited && " and did not exit in 5 s"}; the session's processes were at <b>{toast.tree}</b> of their <b>{toast.budget}</b> budget. The session continues.
            </p>
            <div className="actions">
              <button type="button" className="[ button ]" data-size="sm" onClick={() => void presenter.details(toast.key)}>
                Details
              </button>
            </div>
          </div>
          <button type="button" className="[ button ]" data-variant="ghost" data-size="sm" data-icon aria-label="Dismiss" onClick={() => presenter.dismiss(toast.key)}>
            ×
          </button>
        </div>
      ))}
      {more > 0 && (
        <div className="[ toast ]" data-more>
          +{more} earlier · in Details
        </div>
      )}
    </div>
  );
}
