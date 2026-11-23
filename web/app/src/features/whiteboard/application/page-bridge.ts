// How the board and the task page talk. The page is Alpine's (the session list,
// the pane); the board is React's. Each side only sends and listens.

import type { AgentsView } from "../domain/view";

/** What the board tells the page: which view to show, and which session to select with it. */
export interface ViewHost {
  showView(view: AgentsView, sessionId: string): void;
}

/** What the page tells the board. Each `follow` returns what stops it. */
export interface PageSignals {
  /** The person chose a view with the toggle in the page. */
  followViewChoice(onChoice: (view: AgentsView) => void): () => void;
  /** The page selected a session (or none): its list, its Back button, a feed change. */
  followSelection(onSelect: (sessionId: string | null) => void): () => void;
}
