// The agents whiteboard, passive: it renders the presenter's view model and
// forwards the person's actions to it (ADR 0108). Classes come from the
// design system's whiteboard block.

import { usePresenter } from "../../../../shared/presentation/use-presenter";
import type { BoardPresenter } from "../presenters/board-presenter";
import { CardView } from "./card-view";

export function BoardView({ presenter }: { presenter: BoardPresenter }) {
  const vm = usePresenter(presenter);
  const width = Math.max(0, ...vm.cards.map((c) => c.rect.x + c.rect.w)) + 48;
  const height = Math.max(0, ...vm.cards.map((c) => c.rect.y + c.rect.h)) + 48;
  return (
    <section className="whiteboard" aria-label="Agents whiteboard" data-layout="rank">
      <div className="wb-canvas" tabIndex={0} role="group" aria-roledescription="whiteboard" aria-label={`${vm.cards.length - 1} sessions as a graph.`}>
        <div className="wb-world" role="list">
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
