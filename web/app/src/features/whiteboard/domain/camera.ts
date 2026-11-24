// The camera of the board: where the board sits in the viewport and how big.
// A board point p is drawn at p * scale + (x, y).

import type { Point, Rect, Size } from "./layout";

export interface Camera {
  x: number;
  y: number;
  scale: number;
}

/** Low enough that a board of many columns still fits a phone; the cards are compact there. */
export const MIN_SCALE = 0.15;
export const MAX_SCALE = 2;
const FIT_MARGIN = 16;

const clamp = (scale: number): number => Math.min(MAX_SCALE, Math.max(MIN_SCALE, scale));

export function panBy(camera: Camera, dx: number, dy: number): Camera {
  return { ...camera, x: camera.x + dx, y: camera.y + dy };
}

/** Zooms by `factor`, keeping the board point under `at` (viewport px) where it is. */
export function zoomAt(camera: Camera, factor: number, at: Point): Camera {
  const scale = clamp(camera.scale * factor);
  const k = scale / camera.scale;
  return { scale, x: at.x - (at.x - camera.x) * k, y: at.y - (at.y - camera.y) * k };
}

/** Frames the whole board in the viewport, never magnified past 100 percent. */
export function fit(board: Size, viewport: Size): Camera {
  const scale = clamp(Math.min(1, (viewport.w - 2 * FIT_MARGIN) / board.w, (viewport.h - 2 * FIT_MARGIN) / board.h));
  return { scale, x: (viewport.w - board.w * scale) / 2, y: Math.max(FIT_MARGIN, (viewport.h - board.h * scale) / 2) };
}

/** Pans so `rect` is in the middle of the viewport; the scale stays. */
export function centerOn(camera: Camera, rect: Rect, viewport: Size): Camera {
  return { ...camera, x: viewport.w / 2 - (rect.x + rect.w / 2) * camera.scale, y: viewport.h / 2 - (rect.y + rect.h / 2) * camera.scale };
}

/** Whether any part of `rect` shows in the viewport. */
export function isVisible(camera: Camera, rect: Rect, viewport: Size): boolean {
  const left = rect.x * camera.scale + camera.x;
  const top = rect.y * camera.scale + camera.y;
  return left < viewport.w && top < viewport.h && left + rect.w * camera.scale > 0 && top + rect.h * camera.scale > 0;
}
