import type { ContextPresenter } from "../presenters/context-presenter";
import type { InstructionsGroup } from "../presenters/context-view-model";
import { GroupView } from "./group-view";

export function InstructionsView({ group, presenter }: { group: InstructionsGroup; presenter: ContextPresenter }) {
  return (
    <GroupView heading={group} onToggle={() => presenter.toggleGroup("instructions")}>
      <ul className="[ rows ]">
        {group.rows.map((row) => (
          <li key={row.id} className="[ row ]" data-open={row.open}>
            <button type="button" className="[ summary-row ]" aria-expanded={row.open} onClick={() => presenter.togglePiece(row.id)}>
              <span className="[ name ]">{row.title}</span>
              <span className="[ muted ]">{row.source}</span>
              {row.matchedInText && <span className="[ badge ]">matches in text</span>}
              {row.changed !== "" && <span className="[ badge ]">{row.changed}</span>}
              {row.version !== "" && <span className="[ badge ]">{row.version}</span>}
              <span className="[ muted ]">{row.size}</span>
            </button>
            {row.open && (
              <div className="[ detail ]">
                {row.path !== "" && <p className="[ path ] [ mono ]">{row.path}</p>}
                <pre className="[ text ]">{row.text}</pre>
                {row.canShowAll && (
                  <button type="button" className="[ button ]" data-size="sm" disabled={row.fullText === "loading"} onClick={() => void presenter.showAll(row.id)}>
                    {row.fullText === "failed" ? "Retry Show all" : "Show all"}
                  </button>
                )}
                {row.fullText === "failed" && <p className="[ reason ]">The full text could not be loaded.</p>}
              </div>
            )}
          </li>
        ))}
      </ul>
    </GroupView>
  );
}
