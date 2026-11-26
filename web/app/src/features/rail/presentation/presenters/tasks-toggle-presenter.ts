// The header's Tasks button: below 48rem it opens the rail as a drawer, and
// says how many tasks need the person. It shares the rail's state (the badge and
// whether the drawer is open) through the rail's presenter once that exists.

import { Presenter } from "../../../../shared/kernel/presenter";
import type { RailPresenter } from "./rail-presenter";

export interface TasksToggleViewModel {
  readonly needsYouCount: number;
  readonly hasNeedsYou: boolean;
  /** The drawer is open: the button's aria-expanded. */
  readonly expanded: boolean;
}

const viewModel = (needsYouCount: number, expanded: boolean): TasksToggleViewModel => ({ needsYouCount, hasNeedsYou: needsYouCount > 0, expanded });

export class TasksTogglePresenter extends Presenter<TasksToggleViewModel> {
  #follows: (() => void) | null = null;

  /** `needsYou` is what the server counted: the count until the rail's own takes over. */
  constructor(needsYou: number) {
    super(viewModel(needsYou, false));
  }

  /** Shares the rail's state from now on. */
  follow(rail: RailPresenter): void {
    this.#follows?.();
    this.#follows = rail.subscribe((vm) => this.set(viewModel(vm.needsYouCount, vm.drawerOpen)));
  }

  stop(): void {
    this.#follows?.();
    this.#follows = null;
  }
}
