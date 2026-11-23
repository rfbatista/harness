import type { ContextPresenter } from "../presenters/context-presenter";
import type { HooksGroup, SkillsGroup } from "../presenters/context-view-model";
import { GroupView } from "./group-view";

export function SkillsView({ group, presenter }: { group: SkillsGroup; presenter: ContextPresenter }) {
  return (
    <GroupView heading={group} onToggle={() => presenter.toggleGroup("skills")}>
      <ul className="[ rows ]">
        {group.rows.map((row) => (
          <li key={row.name} className="[ row ]" data-ring={row.ringing ? "true" : undefined}>
            <span className="[ name ] [ mono ]">{row.name}</span>
            <span className="[ badge ]">{row.source}</span>
            {row.invoked !== "" && <span className="[ used ]">{row.invoked}</span>}
            <span className="[ muted meaning ]">{row.description}</span>
          </li>
        ))}
      </ul>
    </GroupView>
  );
}

export function HooksView({ group, presenter }: { group: HooksGroup; presenter: ContextPresenter }) {
  return (
    <GroupView heading={group} onToggle={() => presenter.toggleGroup("hooks")}>
      <ul className="[ rows ]">
        {group.rows.map((row, i) => (
          <li key={i} className="[ row ]">
            <span className="[ name ] [ mono ]">{row.event}</span>
            {row.matcher !== "" && <span className="[ badge ] [ mono ]">{row.matcher}</span>}
            <span className="[ muted meaning ]">{row.rule}</span>
          </li>
        ))}
      </ul>
    </GroupView>
  );
}
