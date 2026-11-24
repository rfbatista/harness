// The product owner's dock: whether the panel is open beside the page, how wide it is, which
// project it belongs to and whether that project's product owner is on. The top bar's toggle
// and the panel share one dock for the whole document. A page change that swaps only <main>
// keeps it; one that swaps the whole frame binds it to the next project, and what the browser
// remembered (open, width, the open thread) comes back.

import type { FeedStatus } from "../domain/feed-status";
import type { ProductOwnerEvents, ProductOwnerGateway, DockMemory, PageNavigator, Screen } from "./ports";
import { pageHref } from "./links";
import { ProductOwnerError } from "./ports";

export const MIN_WIDTH = 22;
export const MAX_WIDTH = 40;
const DEFAULT_WIDTH = 27;

export interface DockState {
  /** "" while no project page is open. */
  readonly projectId: string;
  readonly open: boolean;
  /** Open, on a screen wide enough: the panel is beside the page. */
  readonly docked: boolean;
  /** In rem. */
  readonly width: number;
  /** The thread the panel shows, from what was remembered; null for a fresh one. */
  readonly threadId: string | null;
  /** null until the project's switch is read. */
  readonly enabled: boolean | null;
  readonly narrow: boolean;
  /** Counts the times the composer is asked to take focus. */
  readonly focusComposer: number;
  /** Counts the times the toggle is asked to take focus. */
  readonly focusToggle: number;
  readonly error: string;
}

export interface DockDeps {
  readonly gateway: ProductOwnerGateway;
  readonly events: ProductOwnerEvents;
  readonly memory: DockMemory;
  readonly screen: Screen;
  readonly navigator: PageNavigator;
}

const clamp = (rem: number): number => Math.min(MAX_WIDTH, Math.max(MIN_WIDTH, rem));

export class OwnerDock {
  readonly #deps: DockDeps;
  readonly #listeners = new Set<(state: DockState) => void>();
  #state: DockState;
  #binding = 0;
  #follow: (() => void) | null = null;
  #watchScreen: (() => void) | null = null;
  #open = false;
  #threadId: string | null = null;

  constructor(deps: DockDeps) {
    this.#deps = deps;
    this.#state = this.#derive({ projectId: "", width: DEFAULT_WIDTH, threadId: null, enabled: null, focusComposer: 0, focusToggle: 0, error: "" });
  }

  get state(): DockState {
    return this.#state;
  }

  subscribe = (listener: (state: DockState) => void): (() => void) => {
    this.#listeners.add(listener);
    listener(this.#state);
    return () => void this.#listeners.delete(listener);
  };

  /** Binds the dock to a project page; the returned function releases it. A later bind replaces this one. */
  bind(projectId: string): () => void {
    const token = ++this.#binding;
    this.#stopFollowing();
    const remembered = this.#deps.memory.load(projectId);
    this.#open = remembered.open;
    this.#threadId = remembered.threadId;
    this.#watchScreen ??= this.#deps.screen.onChange(() => this.#set({}));
    this.#set({ projectId, width: clamp(remembered.width), threadId: remembered.threadId, enabled: null, error: "" });
    this.#follow = this.#deps.events.follow(
      projectId,
      (event) => {
        if (event.type === "settings" && event.settings.projectId === projectId) this.#set({ enabled: event.settings.enabled });
      },
      (status: FeedStatus) => {
        if (status === "resynced") void this.#read(token, projectId);
      },
    );
    void this.#read(token, projectId);
    return () => {
      if (token !== this.#binding) return;
      this.#stopFollowing();
      this.#watchScreen?.();
      this.#watchScreen = null;
      this.#set({ projectId: "", enabled: null, error: "" });
    };
  }

  /** The toggle: opens or closes the panel, or on a narrow screen opens the page. */
  toggle(): void {
    const { projectId } = this.#state;
    if (projectId === "") return;
    if (this.#deps.screen.narrow()) {
      this.#deps.navigator.open(pageHref(projectId));
      return;
    }
    if (this.#open) this.close();
    else this.open();
  }

  open(): void {
    if (this.#state.projectId === "") return;
    this.#open = true;
    this.#remember();
    this.#set({ focusComposer: this.#state.focusComposer + 1 });
  }

  /** `returnFocus`: the person closed it from inside, so the toggle takes focus back. */
  close(options: { returnFocus?: boolean } = {}): void {
    this.#open = false;
    this.#remember();
    this.#set(options.returnFocus ? { focusToggle: this.#state.focusToggle + 1 } : {});
  }

  setWidth(rem: number): void {
    this.#set({ width: clamp(rem) });
    this.#remember();
  }

  /** The thread the panel shows changed: remembered for the next page load. */
  setThreadId(threadId: string | null): void {
    this.#threadId = threadId;
    this.#set({ threadId });
    this.#remember();
  }

  /** "Turn it on": flips the project's switch. */
  async turnOn(): Promise<void> {
    const { projectId } = this.#state;
    if (projectId === "") return;
    try {
      const settings = await this.#deps.gateway.setEnabled(projectId, true);
      if (this.#state.projectId === projectId) this.#set({ enabled: settings.enabled, error: "" });
    } catch (err) {
      if (!(err instanceof ProductOwnerError)) throw err;
      this.#set({ error: err.message });
    }
  }

  async #read(token: number, projectId: string): Promise<void> {
    try {
      const settings = await this.#deps.gateway.settings(projectId);
      if (token === this.#binding) this.#set({ enabled: settings.enabled });
    } catch (err) {
      // The switch stays unknown: the toggle looks on, and a send says why it cannot.
      if (!(err instanceof ProductOwnerError)) throw err;
    }
  }

  #stopFollowing(): void {
    this.#follow?.();
    this.#follow = null;
  }

  #remember(): void {
    const { projectId, width } = this.#state;
    if (projectId !== "") this.#deps.memory.save(projectId, { open: this.#open, width, threadId: this.#threadId });
  }

  #derive(partial: Omit<DockState, "open" | "docked" | "narrow">): DockState {
    const narrow = this.#deps.screen.narrow();
    return { ...partial, open: this.#open, docked: this.#open && !narrow && partial.projectId !== "", narrow };
  }

  #set(patch: Partial<DockState>): void {
    const { projectId, width, threadId, enabled, focusComposer, focusToggle, error } = this.#state;
    this.#state = this.#derive({ projectId, width, threadId, enabled, focusComposer, focusToggle, error, ...patch });
    for (const listener of [...this.#listeners]) listener(this.#state);
  }
}
