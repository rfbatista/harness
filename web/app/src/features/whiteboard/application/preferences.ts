// What the person chose for a task's board in this browser (ADR 0037, ADR 0108).

import type { Camera } from "../domain/camera";
import type { AgentsView } from "../domain/view";
import type { Point } from "../domain/layout";

export interface BoardPreferences {
  /** The positions the person gave cards, by session id; empty when none, or unreadable. */
  positions(ticketId: string): Record<string, Point>;
  /** An empty record forgets them all. */
  savePositions(ticketId: string, positions: Record<string, Point>): void;
  /** Where the person left the camera; null when never, or unreadable. */
  camera(ticketId: string): Camera | null;
  saveCamera(ticketId: string, camera: Camera): void;
  /** Which view of the task's sessions the person left, or null when none was chosen (or it is unreadable): the list. */
  view(ticketId: string): AgentsView | null;
  saveView(ticketId: string, view: AgentsView): void;
}
