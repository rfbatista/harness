// The docked panel's frame: its head, its width, and what it says while the product owner is
// off. The thread inside it is the thread component's (a later ticket).

import { Presenter } from "../../../../shared/kernel/presenter";
import type { Conversation } from "../../application/conversation";
import { type DockState, type OwnerDock } from "../../application/owner-dock";
import type { ThreadPresenter } from "./thread-presenter";

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
  readonly #conversation: Conversation;
  readonly thread: ThreadPresenter;
  #attached = "";
  #detach: (() => void) | null = null;

  /** The conversation follows the panel: it is read while the panel is docked, and let go when it closes. */
  constructor(dock: OwnerDock, conversation: Conversation, thread: ThreadPresenter) {
    super(panelViewModel(dock.state));
    this.#dock = dock;
    this.#conversation = conversation;
    this.thread = thread;
    this.#unsubscribe = dock.subscribe((state) => {
      this.set(panelViewModel(state));
      this.thread.setOff(state.enabled === false);
      this.#follow(state);
    });
  }

  readonly #unsubscribe: () => void;

  /** The panel's island goes away: nothing of it stays on the stream. */
  destroy(): void {
    this.#unsubscribe();
    this.#detach?.();
    this.#detach = null;
    this.#attached = "";
  }

  #follow(state: DockState): void {
    const wanted = state.docked ? state.projectId : "";
    if (wanted === this.#attached) return;
    this.#detach?.();
    // Marked first: attaching tells the dock the thread, and the dock tells us.
    this.#attached = wanted;
    this.#detach = wanted === "" ? null : this.#conversation.attach(wanted, state.threadId);
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
