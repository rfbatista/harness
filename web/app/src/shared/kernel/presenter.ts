// The base of every presenter: framework-free state holder that pushes its
// view model to subscribers. A view subscribes and renders what it is given.

export type Unsubscribe = () => void;

export class Presenter<VM> {
  #viewModel: VM;
  readonly #listeners = new Set<(viewModel: VM) => void>();

  constructor(initial: VM) {
    this.#viewModel = initial;
  }

  /** The current view model (also the snapshot React reads while rendering). */
  get viewModel(): VM {
    return this.#viewModel;
  }

  /** Calls `listener` with the current view model, then with every change. */
  subscribe = (listener: (viewModel: VM) => void): Unsubscribe => {
    this.#listeners.add(listener);
    listener(this.#viewModel);
    return () => {
      this.#listeners.delete(listener);
    };
  };

  protected set(next: VM): void {
    this.#viewModel = next;
    for (const listener of [...this.#listeners]) listener(next);
  }

  protected update(change: (current: VM) => VM): void {
    this.set(change(this.#viewModel));
  }
}
