// Which view of the task's sessions the page shows (ADR 0108): the list unless
// the address asks for the board (`view=whiteboard&session=<id>`, the link the
// Context tab uses) or the person left the board last time. Framework-free; it
// has no view of its own: the page shows the view it announces.

import { Presenter } from "../../../../shared/kernel/presenter";
import type { PageSignals, ViewHost } from "../../application/page-bridge";
import type { BoardPreferences } from "../../application/preferences";
import { AGENTS_VIEWS, type AgentsView } from "../../domain/view";

export interface ViewModel {
  view: AgentsView;
}

export interface ViewDeps {
  ticketId: string;
  preferences: BoardPreferences;
  host: ViewHost;
  signals: PageSignals;
  /** The page's query string: "?view=whiteboard&session=s1". */
  search: string;
}

export class ViewPresenter extends Presenter<ViewModel> {
  readonly #deps: ViewDeps;
  #unfollow: (() => void) | null = null;

  constructor(deps: ViewDeps) {
    super({ view: "list" });
    this.#deps = deps;
  }

  /** Announces the first view and follows the person's choices. */
  start(): void {
    const query = new URLSearchParams(this.#deps.search);
    const asked = query.get("view");
    const linked = (AGENTS_VIEWS as readonly string[]).includes(asked ?? "") ? (asked as AgentsView) : null;
    // A link opens the board with its session selected; it does not change what the person chose for next time.
    const view = linked ?? this.#deps.preferences.view(this.#deps.ticketId) ?? "list";
    this.#show(view, view === "whiteboard" && linked ? (query.get("session") ?? "") : "");
    this.#unfollow ??= this.#deps.signals.followViewChoice((chosen) => this.choose(chosen));
  }

  /** The person chose a view: it is kept for this task in this browser. */
  choose(view: AgentsView): void {
    this.#deps.preferences.saveView(this.#deps.ticketId, view);
    this.#show(view, "");
  }

  stop(): void {
    this.#unfollow?.();
    this.#unfollow = null;
  }

  #show(view: AgentsView, sessionId: string): void {
    this.set({ view });
    this.#deps.host.showView(view, sessionId);
  }
}
