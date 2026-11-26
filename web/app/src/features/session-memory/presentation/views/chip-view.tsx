import type { CSSProperties } from "react";
import { usePresenter } from "../../../../shared/presentation/use-presenter";
import type { ChipPresenter } from "../presenters/chip-presenter";

/** The memory chip of the session head: nothing until the session has a sample. */
export function ChipView({ presenter }: { presenter: ChipPresenter }) {
  const { view } = usePresenter(presenter);
  if (!view) return null;
  return (
    <button type="button" className="[ memory ]" data-state={view.state} aria-label={view.title} title={view.title} style={{ "--_fill": `${view.fillPercent}%` } as CSSProperties}>
      <span className="num">
        {view.num}
        <span className="of">{view.of}</span>
      </span>
      {view.word && <span className="word">{view.word}</span>}
      <span className="track" aria-hidden="true">
        <span className="fill" />
      </span>
    </button>
  );
}
