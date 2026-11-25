import { useEffect } from "react";
import { usePresenter } from "../../../../shared/presentation/use-presenter";
import type { TasksTogglePresenter } from "../presenters/tasks-toggle-presenter";
import { RailIcon } from "./rail-icons";

/**
 * The Tasks button's contents. `host` is the button's own element: the island's
 * mount, which the server draws as the link to the board (null on the server).
 * A click opens the drawer; without JavaScript the link goes to the board.
 */
export function TasksToggleView({ presenter, host, onOpen }: { presenter: TasksTogglePresenter; host: HTMLElement | null; onOpen(): void }) {
  const vm = usePresenter(presenter);

  useEffect(() => {
    host?.setAttribute("aria-expanded", vm.expanded ? "true" : "false");
  }, [host, vm.expanded]);

  useEffect(() => {
    if (!host) return;
    const open = (event: Event) => {
      event.preventDefault();
      onOpen();
    };
    host.addEventListener("click", open);
    return () => host.removeEventListener("click", open);
  }, [host, onOpen]);

  return (
    <>
      <RailIcon name="rail" />
      Tasks
      <span className="[ badge ]" data-tone="attention" style={vm.hasNeedsYou ? undefined : { display: "none" }}>
        <span>{vm.needsYouCount}</span>
        <span className="[ visually-hidden ]"> need you</span>
      </span>
    </>
  );
}
