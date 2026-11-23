import { useEffect, useRef, useState } from "react";
import { usePresenter } from "../../../../shared/presentation/use-presenter";
import type { TogglePresenter } from "../presenters/toggle-presenter";
import { OwnerMark } from "./owner-icon";

/** The "Product owner" toggle after the project picker. ⌥⇧O toggles it from anywhere on the page. */
export function ToggleView({ presenter }: { presenter: TogglePresenter }) {
  const vm = usePresenter(presenter);
  const button = useRef<HTMLButtonElement>(null);
  const seenFocus = useRef(vm.focusRequest);
  // What the server rendered knows nothing of the remembered panel: it shows it open only after hydration.
  const [hydrated, setHydrated] = useState(false);
  useEffect(() => setHydrated(true), []);

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (presenter.handleKey(event)) event.preventDefault();
    };
    // The page's own window: the toggle is on every page, and its key works from anywhere in it.
    const win = button.current?.ownerDocument.defaultView;
    win?.addEventListener("keydown", onKey);
    return () => win?.removeEventListener("keydown", onKey);
  }, [presenter, vm.visible]);

  // The panel was closed from inside: focus comes back here.
  useEffect(() => {
    if (vm.focusRequest !== seenFocus.current) button.current?.focus();
    seenFocus.current = vm.focusRequest;
  }, [vm.focusRequest]);

  if (!vm.visible) return null;
  return (
    <button
      ref={button}
      type="button"
      className="[ button ] [ po-toggle ]"
      data-variant="ghost"
      data-size="sm"
      aria-pressed={hydrated && vm.pressed}
      aria-label={vm.accessibleName}
      aria-keyshortcuts={vm.shortcut}
      title={`${vm.label} (⌥⇧O)`}
      data-off={vm.off ? "" : undefined}
      onClick={presenter.toggle}
    >
      <OwnerMark size="sm" off={vm.off} />
      <span className="[ label ]">{vm.label}</span>
    </button>
  );
}
