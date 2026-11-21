import { Icon } from "../../../../shared/presentation/icon";
import { usePresenter } from "../../../../shared/presentation/use-presenter";
import type { BellPresenter } from "../presenters/bell-presenter";

/** The bell, without JavaScript a link to the inbox. The panel joins it in ticket 05. */
export function BellView({ presenter }: { presenter: BellPresenter }) {
  const vm = usePresenter(presenter);
  if (!vm.visible) return null;
  return (
    <div className="[ notifications ]" data-feed={vm.feed}>
      <a className="[ button ] [ bell ]" data-variant="ghost" data-icon="" href={vm.href} aria-label={vm.label} aria-controls="notifications-panel">
        <Icon name="bell" />
        <span className="[ badge ]" data-tone="attention" aria-hidden="true" style={vm.hasUnread ? undefined : { display: "none" }}>
          {vm.badge}
        </span>
      </a>
    </div>
  );
}
