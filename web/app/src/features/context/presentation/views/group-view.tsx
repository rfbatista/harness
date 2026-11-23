import type { ReactNode } from "react";
import type { GroupHeading } from "../presenters/context-view-model";

/** A group's heading as a disclosure button, with its count, and its body while open. */
export function GroupView({ heading, onToggle, children }: { heading: GroupHeading; onToggle: () => void; children: ReactNode }) {
  const id = `context-group-${heading.key}`;
  return (
    <section className="[ part ]" data-group={heading.key} aria-labelledby={`${id}-title`}>
      <h3 id={`${id}-title`}>
        <button type="button" className="[ group-toggle ]" aria-expanded={heading.open} aria-controls={id} onClick={onToggle}>
          {heading.title} <span className="[ summary ]">· {heading.count}</span>
        </button>
      </h3>
      {heading.open && <div id={id}>{children}</div>}
    </section>
  );
}
