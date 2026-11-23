// The agents whiteboard, passive: it renders the presenter's view model and
// forwards the person's actions to it (ADR 0108). Classes come from the
// design system's whiteboard block.

import { usePresenter } from "../../../../shared/presentation/use-presenter";
import type { BoardPresenter } from "../presenters/board-presenter";
import { CardView } from "./card-view";

/** The stroke each edge tone takes from the design system block. */
const KIND = { started: "started", signal: "messages", amber: "review" } as const;

export function BoardView({ presenter }: { presenter: BoardPresenter }) {
  const vm = usePresenter(presenter);
  const { w: width, h: height } = vm.canvas;
  return (
    <section className="whiteboard" aria-label="Agents whiteboard" data-layout="rank" onKeyDown={(event) => event.key.toLowerCase() === "t" && !event.metaKey && !event.ctrlKey && presenter.tidy()}>
      <div className="wb-bar">
        <div className="wb-tools">
          <button type="button" className="button" data-variant="ghost" data-size="sm" data-tidy disabled={!vm.tidyEnabled} title="Tidy (T): put every card back in its computed place" onClick={presenter.tidy}>
            Tidy
            {vm.placedCount > 0 && <span className="badge">{vm.placedCount} placed</span>}
          </button>
        </div>
      </div>
      <div className="wb-canvas" tabIndex={0} role="group" aria-roledescription="whiteboard" aria-label={`${vm.cards.length - 1} sessions as a graph.`}>
        <div className="wb-world" role="list" data-compact={vm.compact ? "" : undefined} style={{ inlineSize: width, blockSize: height }}>
          {vm.columns.map((column) => (
            <div key={column.key} className="wb-lane" aria-hidden="true" style={{ left: column.x, top: column.y, inlineSize: column.w }}>
              {column.label} <span className="count">{column.count}</span>
            </div>
          ))}
          <svg className="wb-edges" width={width} height={height} aria-hidden="true">
            {vm.edges.map((e) => (
              <path key={e.id} d={e.d} data-kind={KIND[e.tone]} data-dim={e.dimmed ? "" : undefined} />
            ))}
          </svg>
          {vm.edges.map((e) =>
            e.badge ? (
              <button key={e.id} type="button" className="wb-pill" data-edge={e.id} data-tone={e.badge.tone || undefined} style={{ left: e.badge.x, top: e.badge.y }} title={e.badge.name} aria-label={e.badge.name}>
                <span className="n">{e.badge.count}</span>
                <span className="latest">{e.badge.text}</span>
              </button>
            ) : null,
          )}
          {vm.cards.map((card) => (
            <CardView key={card.id} card={card} presenter={presenter} />
          ))}
        </div>
      </div>
    </section>
  );
}
