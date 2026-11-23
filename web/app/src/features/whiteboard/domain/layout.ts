// Where cards sit: a pure function of the task's topology and start order.

import { inStartOrder, type BoardSession } from "./session";
import { YOU_ID } from "./graph";

export interface Point {
  x: number;
  y: number;
}

export interface Size {
  w: number;
  h: number;
}

/** A card's size in px: the session row as a tile (15.5rem), its compact form (11rem), and the You card. */
export const FULL: Size = { w: 248, h: 112 };
export const COMPACT: Size = { w: 176, h: 58 };
export const YOU: Size = { w: 176, h: 40 };
export const YOU_COMPACT: Size = { w: 120, h: 40 };

export const sizeOf = (you: boolean, compact: boolean): Size => (you ? (compact ? YOU_COMPACT : YOU) : compact ? COMPACT : FULL);
const GAP = 24;

export interface Layout {
  positions: Record<string, Point>;
}

/** A first placement: a single column in start order. Replaced by the column layout. */
export function computeLayout(sessions: readonly BoardSession[], size: Size): Layout {
  const positions: Record<string, Point> = { [YOU_ID]: { x: 0, y: 0 } };
  inStartOrder(sessions).forEach((s, i) => {
    positions[s.id] = { x: size.w + 72, y: i * (size.h + GAP) };
  });
  return { positions };
}

export interface Rect extends Point, Size {}

export interface EdgeGeometry {
  /** The SVG path from the right side of the source to the left side of the target. */
  d: string;
  /** Where a thread badge sits: near the target's end of the line. */
  badge: Point;
}

/** A smooth path between two cards, with horizontal handles. */
export function edgeGeometry(from: Rect, to: Rect): EdgeGeometry {
  const a = { x: from.x + from.w, y: from.y + from.h / 2 };
  const b = { x: to.x, y: to.y + to.h / 2 };
  const dx = Math.max(32, Math.abs(b.x - a.x) / 2);
  const c1 = { x: a.x + dx, y: a.y };
  const c2 = { x: b.x - dx, y: b.y };
  const t = 0.78;
  const u = 1 - t;
  const at = (p0: number, p1: number, p2: number, p3: number) => u * u * u * p0 + 3 * u * u * t * p1 + 3 * u * t * t * p2 + t * t * t * p3;
  return {
    d: `M${a.x},${a.y} C${c1.x},${c1.y} ${c2.x},${c2.y} ${b.x},${b.y}`,
    badge: { x: at(a.x, c1.x, c2.x, b.x), y: at(a.y, c1.y, c2.y, b.y) },
  };
}
