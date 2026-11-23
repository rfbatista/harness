import type { ContextPresenter } from "../presenters/context-presenter";
import type { ChangesGroup } from "../presenters/context-view-model";
import { GroupView } from "./group-view";

export function ChangesView({ group, presenter }: { group: ChangesGroup; presenter: ContextPresenter }) {
  return (
    <GroupView heading={group} onToggle={() => presenter.toggleGroup("changes")}>
      {group.rows.length === 0 ? (
        <p className="[ muted ]">Nothing has changed since the session started.</p>
      ) : (
        <ul className="[ rows ]">
          {group.rows.map((row, i) => (
            <li key={i} className="[ row ]">
              <time className="[ muted ]">{row.time}</time>
              <span className="[ name ]">{row.text}</span>
              <span className="[ muted ]">{row.note}</span>
            </li>
          ))}
        </ul>
      )}
    </GroupView>
  );
}
