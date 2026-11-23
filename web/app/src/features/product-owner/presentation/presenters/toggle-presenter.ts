// The top bar's "Product owner" toggle: a button right after the project picker that opens
// the docked panel (or, on a narrow screen, the page). It is on every project page.

import { Presenter } from "../../../../shared/kernel/presenter";
import type { DockState, OwnerDock } from "../../application/owner-dock";

export interface ToggleViewModel {
  /** Only on a page of a project. */
  readonly visible: boolean;
  /** The panel is open beside the page. */
  readonly pressed: boolean;
  /** The product owner is switched off: the label is struck through. */
  readonly off: boolean;
  readonly label: string;
  readonly accessibleName: string;
  /** The shortcut, as the tooltip names it. */
  readonly shortcut: string;
  /** Counts the times the toggle is asked to take focus (the panel closed from inside). */
  readonly focusRequest: number;
}

const LABEL = "Product owner";

export const toggleViewModel = (dock: DockState): ToggleViewModel => {
  const off = dock.enabled === false;
  return { visible: dock.projectId !== "", pressed: dock.docked, off, label: LABEL, accessibleName: off ? `${LABEL} (off)` : LABEL, shortcut: "Alt+Shift+O", focusRequest: dock.focusToggle };
};

export interface KeyLike {
  readonly code: string;
  readonly altKey: boolean;
  readonly shiftKey: boolean;
  readonly ctrlKey: boolean;
  readonly metaKey: boolean;
}

export class TogglePresenter extends Presenter<ToggleViewModel> {
  readonly #dock: OwnerDock;

  constructor(dock: OwnerDock) {
    super(toggleViewModel(dock.state));
    this.#dock = dock;
    dock.subscribe((state) => this.set(toggleViewModel(state)));
  }

  toggle = (): void => this.#dock.toggle();

  /** ⌥⇧O from anywhere; true when the key was this one. (The key's code, not its character: ⌥ changes the character on a Mac.) */
  handleKey = (key: KeyLike): boolean => {
    if (key.code !== "KeyO" || !key.altKey || !key.shiftKey || key.ctrlKey || key.metaKey) return false;
    this.#dock.toggle();
    return true;
  };
}
