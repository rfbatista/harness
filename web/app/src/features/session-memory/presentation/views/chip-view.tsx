import { useEffect, useRef, type CSSProperties } from "react";
import { usePopup } from "../../../../shared/presentation/use-popup";
import { usePresenter } from "../../../../shared/presentation/use-presenter";
import type { ChipPresenter } from "../presenters/chip-presenter";

const POPOVER_ID = "memory-popover";

/** The memory chip of the session head, and the popover that hangs from it: nothing until the session has a sample. */
export function ChipView({ presenter }: { presenter: ChipPresenter }) {
  const { view, popover } = usePresenter(presenter);
  const chip = useRef<HTMLButtonElement>(null);
  const panel = useRef<HTMLElement>(null);
  const open = popover !== null;
  usePopup(open, panel, chip, () => presenter.close());

  // Esc closes the popover and gives the focus back to the chip, wherever the focus is.
  useEffect(() => {
    const doc = chip.current?.ownerDocument;
    if (!open || !doc) return;
    const onKey = (event: KeyboardEvent) => {
      if (event.key !== "Escape") return;
      event.stopPropagation();
      presenter.close();
      chip.current?.focus();
    };
    doc.addEventListener("keydown", onKey, true);
    return () => doc.removeEventListener("keydown", onKey, true);
  }, [open, presenter]);

  if (!view) return null;
  return (
    <>
      <button
        ref={chip}
        type="button"
        className="[ memory ]"
        data-state={view.state}
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
        <span className="track" aria-hidden="true">
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
    </>
  );
}
