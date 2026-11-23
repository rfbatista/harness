// The agents whiteboard, passive: it renders the presenter's view model and
// forwards the person's actions to it (ADR 0108). Classes come from the
// design system's whiteboard block.

import { usePresenter } from "../../../../shared/presentation/use-presenter";
import type { BoardPresenter } from "../presenters/board-presenter";
import { CardView } from "./card-view";

export function BoardView({ presenter }: { presenter: BoardPresenter }) {
  const vm = usePresenter(presenter);
  const { w: width, h: height } = vm.canvas;
  return (
    <section className="whiteboard" aria-label="Agents whiteboard" data-layout="rank">
      <div className="wb-canvas" tabIndex={0} role="group" aria-roledescription="whiteboard" aria-label={`${vm.cards.length - 1} sessions as a graph.`}>
        <div className="wb-world" role="list" data-compact={vm.compact ? "" : undefined} style={{ inlineSize: width, blockSize: height }}>
          {vm.columns.map((column) => (
            <div key={column.key} className="wb-lane" aria-hidden="true" style={{ left: column.x, top: column.y, inlineSize: column.w }}>
              {column.label} <span className="count">{column.count}</span>
            </div>
          ))}
          <svg className="wb-edges" width={width} height={height} aria-hidden="true">
            {vm.edges.map((e) => (
              <path key={e.id} d={e.d} data-kind="started" data-dim={e.dimmed ? "" : undefined} />
            ))}
          </svg>
          {vm.cards.map((card) => (
            <CardView key={card.id} card={card} />
          ))}
        </div>
      </div>
    </section>
  );
}
