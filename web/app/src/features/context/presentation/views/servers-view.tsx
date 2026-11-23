import type { ContextPresenter } from "../presenters/context-presenter";
import type { ServersGroup } from "../presenters/context-view-model";
import { GroupView } from "./group-view";

export function ServersView({ group, presenter }: { group: ServersGroup; presenter: ContextPresenter }) {
  return (
    <GroupView heading={group} danger={group.danger} onToggle={() => presenter.toggleGroup("servers")}>
      <ul className="[ rows ]">
        {group.rows.map((row) => (
          <li key={row.name} className="[ row ]" data-state={row.status} data-ring={row.ringing ? "true" : undefined} data-tone={row.failed ? "danger" : undefined}>
            <span className="[ name ] [ mono ]">{row.name}</span>
            <span className="[ status ]">{row.statusLabel}</span>
            <span className="[ badge ]">{row.attached}</span>
            <span className="[ muted ]">{row.attachedReason}</span>
            {row.reason !== "" && <p className="[ reason ]">{row.reason}</p>}
            {row.canCopy && (
              <button type="button" className="[ button ]" data-size="sm" onClick={() => void presenter.copyError(row.name)}>
                {row.copied ? "Copied" : "Copy"}
              </button>
            )}
          </li>
        ))}
      </ul>
    </GroupView>
  );
}
