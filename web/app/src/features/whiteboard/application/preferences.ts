// What the person chose for a task's board in this browser (ADR 0037, ADR 0108).

import type { Point } from "../domain/layout";

export interface BoardPreferences {
  /** The positions the person gave cards, by session id; empty when none, or unreadable. */
  positions(ticketId: string): Record<string, Point>;
  /** An empty record forgets them all. */
  savePositions(ticketId: string, positions: Record<string, Point>): void;
}
