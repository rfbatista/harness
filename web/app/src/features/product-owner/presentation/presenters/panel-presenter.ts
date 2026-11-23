// The docked panel's frame: its head, its width, and what it says while the product owner is
// off. The thread inside it is the thread component's (a later ticket).

import { Presenter } from "../../../../shared/kernel/presenter";
import { type DockState, type OwnerDock } from "../../application/owner-dock";

export interface PanelViewModel {
  readonly docked: boolean;
  readonly projectId: string;
  /** In rem. */
  readonly width: number;
  /** "unknown" until the project's switch is read: the panel looks on. */
  readonly state: "unknown" | "on" | "off";
  readonly composerDisabled: boolean;
  readonly composerPlaceholder: string;
  readonly settingsHref: string;
  /** Why "Turn it on" did not work. */
  readonly error: string;
  /** Counts the times the composer is asked to take focus (the panel opened). */
  readonly focusRequest: number;
}

export const PLACEHOLDER = "Ask about the project, or shape a feature";
export const OFF_PLACEHOLDER = "Turn the product owner on to send a message";

export const panelViewModel = (dock: DockState): PanelViewModel => {
  const state = dock.enabled === null ? "unknown" : dock.enabled ? "on" : "off";
  return {
    docked: dock.docked,
    projectId: dock.projectId,
    width: dock.width,
    state,
    composerDisabled: state === "off",
    composerPlaceholder: state === "off" ? OFF_PLACEHOLDER : PLACEHOLDER,
    settingsHref: `/projects/${encodeURIComponent(dock.projectId)}/settings#product-owner`,
    error: dock.error,
    focusRequest: dock.focusComposer,
  };
};

export class PanelPresenter extends Presenter<PanelViewModel> {
  readonly #dock: OwnerDock;

  constructor(dock: OwnerDock) {
    super(panelViewModel(dock.state));
    this.#dock = dock;
    dock.subscribe((state) => this.set(panelViewModel(state)));
  }

  /** The person closed the panel from inside: focus goes back to the toggle. */
  close = (): void => this.#dock.close({ returnFocus: true });

  turnOn = (): Promise<void> => this.#dock.turnOn();

  resize = (rem: number): void => this.#dock.setWidth(rem);

  /** Esc inside the panel; true when the key was this one. */
  handleKey = (key: { readonly key: string }): boolean => {
    if (key.key !== "Escape") return false;
    this.close();
    return true;
  };

  /** Arrow keys on the resize handle: one rem per press. The panel is on the right, so Left widens it. */
  handleResizerKey = (key: { readonly key: string }): boolean => {
    if (key.key !== "ArrowLeft" && key.key !== "ArrowRight") return false;
    this.resize(this.viewModel.width + (key.key === "ArrowLeft" ? 1 : -1));
    return true;
  };
}
