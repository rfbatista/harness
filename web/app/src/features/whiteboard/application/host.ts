// What the board asks of the page it sits in: the session pane and the person's
// other places. The board never answers a review itself (ADR 0108).

export type PaneTab = "context" | "conversation";

export interface BoardHost {
  /** Opens the session's pane beside the board, on `tab` when given. */
  openSession(sessionId: string, tab?: PaneTab): void;
  /** Closes the pane: the board takes the full width. */
  closePane(): void;
  /** Opens the Reviews popover, where a review request is answered. */
  openReviews(): void;
  newSession(): void;
  showList(): void;
}

/** A host that does nothing: the board before a page is wired to it. */
export const noHost: BoardHost = { openSession() {}, closePane() {}, openReviews() {}, newSession() {}, showList() {} };
