import type { GroupViewModel, RowViewModel } from "../presenters/rail-rows";
import { MergeGlyphs, RailIcon } from "./rail-icons";

const flag = (on: boolean): string | undefined => (on ? "true" : undefined);

/** One task, with its activity: the dot, the title (the filter's match marked), and one meta item. */
function RowLink({ row }: { row: RowViewModel }) {
  return (
    <a href={row.href} data-task-id={row.id} aria-current={row.current ? "page" : undefined} data-attention={flag(row.attention)} data-fresh={flag(row.fresh)} data-peek={flag(row.peek)}>
      {row.isKid ? (
        <span className="[ tdot ]" data-st={row.statusKey} aria-hidden="true" />
      ) : row.hasDot ? (
        <span className="[ status ]" data-dot-only="" data-state={row.state} data-signal={flag(row.signal)} />
      ) : null}
      <span className="[ label ]">
        {row.before}
        {row.hasMatch ? <mark>{row.match}</mark> : null}
        {row.after}
      </span>
      <span className="[ visually-hidden ]">{row.lead}</span>
      {row.hasReviews ? (
        <span className="[ badge ]" data-tone="attention">
          {row.reviewsText}
        </span>
      ) : row.isWaiting ? (
        <span className="[ meta ]" data-tone="attention">
          waiting
        </span>
      ) : row.isLive ? (
        <span className="[ meta ]">{row.liveText}</span>
      ) : row.hasMerge ? (
        <span className="[ meta ]" data-merge-state={row.mergeState}>
          <MergeGlyphs />
          {row.mergeText}
        </span>
      ) : row.hasStatusWord ? (
        <span className="[ meta ]">{row.statusWord}</span>
      ) : null}
      <span className="[ visually-hidden ]">{row.tail}</span>
    </a>
  );
}

/** A list item: a plain task's link, or a feature task's row with its twisty and its children nested under it. */
function RowItem({ row, onToggleFeature }: { row: RowViewModel; onToggleFeature(id: string): void }) {
  if (!row.isFeature) {
    return (
      <li>
        <RowLink row={row} />
      </li>
    );
  }
  return (
    <li data-feature="" data-open={row.open ? "" : undefined}>
      <div className="[ frow ]">
        <RowLink row={row} />
        <button type="button" className="[ twisty ]" data-feature-id={row.id} aria-expanded={row.open} aria-controls={row.kidsId} aria-label={row.twistyLabel} onClick={() => onToggleFeature(row.id)}>
          <span>{row.kidsCount}</span>
          <RailIcon name="chevron" />
        </button>
      </div>
      <ul className="[ kids ]" role="list" id={row.kidsId}>
        {row.kids.map((kid) => (
          <li key={kid.id}>
            <RowLink row={kid} />
          </li>
        ))}
      </ul>
    </li>
  );
}

interface GroupProps {
  group: GroupViewModel;
  turned: boolean;
  onToggleGroup(key: string, heading: HTMLElement): void;
  onToggleFeature(id: string): void;
}

/** A status group: its heading (a disclosure button unless empty or filtering), its rows and Done's rest. */
export function GroupView({ group: g, turned, onToggleGroup, onToggleFeature }: GroupProps) {
  const count = (
    <span className="[ count ]">
      <span>{g.countText}</span>
      <span className="[ visually-hidden ]">{g.countWords}</span>
    </span>
  );
  return (
    <section className="[ group ]" aria-labelledby={g.headingId} data-key={g.key} data-collapsed={flag(g.collapsed)} data-empty={flag(g.empty)}>
      <h3 id={g.headingId}>
        {g.foldable ? (
          <button type="button" aria-expanded={g.expanded} aria-controls={g.listId} data-key={g.key} data-turns={flag(turned)} onClick={(event) => onToggleGroup(g.key, event.currentTarget)}>
            <RailIcon name="chevron" />
            <span className="[ name ]">{g.label}</span>
            {count}
          </button>
        ) : (
          <span className="[ contents ]">
            <span className="[ name ]">{g.label}</span>
            {count}
          </span>
        )}
      </h3>
      <ul role="list" id={g.listId} style={g.hasRows ? undefined : { display: "none" }}>
        {g.rows.map((row) => (
          <RowItem key={row.id} row={row} onToggleFeature={onToggleFeature} />
        ))}
        <li className="[ more ]" style={g.hasMore ? undefined : { display: "none" }}>
          <details>
            <summary>
              <span className="[ closed ]">
                <span>{g.moreText}</span>
                <span className="[ visually-hidden ]"> done tasks</span>
              </span>
              <span className="[ opened ]">Show fewer</span>
            </summary>
            <ul role="list">
              {g.more.map((row) => (
                <RowItem key={row.id} row={row} onToggleFeature={onToggleFeature} />
              ))}
            </ul>
          </details>
        </li>
      </ul>
    </section>
  );
}
