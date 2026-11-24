// Where a card that arrives goes when its computed slot is taken (ADR 0108).

import { ROW_GAP, type Point, type Rect } from "./layout";

const overlaps = (a: Rect, b: Rect): boolean => a.x < b.x + b.w && b.x < a.x + a.w && a.y < b.y + b.h && b.y < a.y + a.h;

/** The first free spot down `start`'s column: `start` itself when nothing covers it. Never moves `taken`. */
export function freeSpot(start: Rect, taken: readonly Rect[]): Point {
  let y = start.y;
  for (let i = 0; i <= taken.length; i++) {
    const hit = taken.find((r) => overlaps({ ...start, y }, r));
    if (!hit) break;
    y = hit.y + hit.h + ROW_GAP;
  }
  return { x: start.x, y };
}
