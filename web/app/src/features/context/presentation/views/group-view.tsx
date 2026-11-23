import type { ReactNode } from "react";
import type { GroupHeading } from "../presenters/context-view-model";

/** A group's heading as a disclosure button, with its count, and its body while open. */
export function GroupView({ heading, danger = false, onToggle, children }: { heading: GroupHeading; /** The count is a failure count: the danger tone. */ danger?: boolean; onToggle: () => void; children: ReactNode }) {
  const id = `context-group-${heading.key}`;
  return (
    <section className="[ part ]" data-group={heading.key} aria-labelledby={`${id}-title`}>
      <h3 id={`${id}-title`}>
        <button type="button" className="[ group-toggle ]" aria-expanded={heading.open} aria-controls={id} onClick={onToggle}>
          {heading.title} <span className="[ summary ]" data-tone={danger ? "danger" : undefined}>
            · {heading.count}
          </span>
        </button>
      </h3>
      {heading.open && <div id={id}>{heading.noMatch ? <p className="[ no-match ]">No match</p> : children}</div>}
    </section>
  );
}
