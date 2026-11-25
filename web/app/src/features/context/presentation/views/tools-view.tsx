import type { ContextPresenter } from "../presenters/context-presenter";
import type { ToolsGroup } from "../presenters/context-view-model";
import { GroupView } from "./group-view";

export function ToolsView({ group, presenter }: { group: ToolsGroup; presenter: ContextPresenter }) {
  return (
    <GroupView heading={group} onToggle={() => presenter.toggleGroup("tools")}>
      {group.sections.map((section) => (
        <div key={section.title} className="[ sub ]">
          <h4>
            {section.title} <span className="[ summary ]">· {section.count}</span>
          </h4>
          <ul className="[ rows ]">
            {section.rows.map((row) => (
              <li key={row.fullName} className="[ row ]" data-state={row.state} data-ring={row.ringing ? "true" : undefined} title={row.fullName}>
                <span className="[ name ] [ mono ]">{row.name}</span>
                <span className="[ status ]">{row.stateLabel}</span>
                {row.used !== "" && <span className="[ used ]">{row.used}</span>}
              </li>
            ))}
          </ul>
        </div>
      ))}
    </GroupView>
  );
}
