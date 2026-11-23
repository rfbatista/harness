import { useEffect, useRef, type FocusEvent, type MouseEvent } from "react";
import { Icon } from "../../../../shared/presentation/icon";
import { usePresenter } from "../../../../shared/presentation/use-presenter";
import type { BellPresenter } from "../presenters/bell-presenter";
import type { PanelPresenter } from "../presenters/panel-presenter";
import { PanelView } from "./panel-view";

/** The bell: without JavaScript a link to the inbox, with it the trigger of the panel. */
export function BellView({ presenter, panel }: { presenter: BellPresenter; panel: PanelPresenter }) {
  const vm = usePresenter(presenter);
  const panelVm = usePresenter(panel);
  const trigger = useRef<HTMLAnchorElement>(null);

  const focus = panelVm.focus;
  useEffect(() => {
    if (focus?.target === "bell") trigger.current?.focus();
  }, [focus]);

  if (!vm.visible) return null;

  const toggle = (event: MouseEvent) => {
    event.preventDefault();
    panel.toggle();
  };
  // Focus that leaves both the bell and the panel closes the panel.
  const focusLeft = (event: FocusEvent<HTMLElement>) => {
    const to = event.relatedTarget;
    if (to instanceof Node && !event.currentTarget.contains(to)) panel.focusMoved(true);
  };

  return (
    <div className="[ notifications ]" data-feed={vm.feed} onBlur={focusLeft}>
      <a ref={trigger} className="[ button ] [ bell ]" data-variant="ghost" data-icon="" href={vm.href} aria-label={vm.label} aria-controls="notifications-panel" aria-expanded={panelVm.open} onClick={toggle}>
        <Icon name="bell" />
        <span className="[ badge ]" data-tone="attention" aria-hidden="true" style={vm.hasBadge ? undefined : { display: "none" }}>
          {vm.badge}
        </span>
      </a>
      <PanelView presenter={panel} anchor={trigger} />
    </div>
  );
}
