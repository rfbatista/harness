import { useSyncExternalStore } from "react";

/** What a view needs from a presenter: its view model now, and changes to it. */
export interface ViewModelSource<VM> {
  readonly viewModel: VM;
  subscribe(listener: (viewModel: VM) => void): () => void;
}

/** The presenter's view model, re-rendered on every change. The server render reads it once. */
export function usePresenter<VM>(presenter: ViewModelSource<VM>): VM {
  const snapshot = () => presenter.viewModel;
  return useSyncExternalStore((onChange) => presenter.subscribe(onChange), snapshot, snapshot);
}
