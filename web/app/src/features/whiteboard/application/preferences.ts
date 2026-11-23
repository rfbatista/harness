// What the person chose for a task's board in this browser (ADR 0037, ADR 0108).

import type { Camera } from "../domain/camera";
import type { Point } from "../domain/layout";

export interface BoardPreferences {
  /** The positions the person gave cards, by session id; empty when none, or unreadable. */
  positions(ticketId: string): Record<string, Point>;
  /** An empty record forgets them all. */
  savePositions(ticketId: string, positions: Record<string, Point>): void;
  /** Where the person left the camera; null when never, or unreadable. */
  camera(ticketId: string): Camera | null;
  saveCamera(ticketId: string, camera: Camera): void;
}
