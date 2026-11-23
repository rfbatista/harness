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
export const FULL: Size = { w: 248, h: 140 };
export const COMPACT: Size = { w: 176, h: 58 };
export const YOU: Size = { w: 176, h: 40 };
export const YOU_COMPACT: Size = { w: 120, h: 40 };

export const sizeOf = (you: boolean, compact: boolean): Size => (you ? (compact ? YOU_COMPACT : YOU) : compact ? COMPACT : FULL);

/** The room between columns holds the edges and their thread badges, which are up to 13rem wide (their latest message is hidden on compact cards). */
export const columnGap = (compact: boolean): number => (compact ? 112 : 224);
export const ROW_GAP = 24;
export const MARGIN = 24;
/** The band above the cards that holds a column's heading. */
export const HEADING = 32;

export interface Column {
  key: string;
  label: string;
  count: number;
  x: number;
  y: number;
  w: number;
}

export interface Layout {
  /** Top-left of each card, by id. */
  positions: Record<string, Point>;
  columns: Column[];
  /** The id of the card in each column slot, top to bottom, by column key. */
  slots: Record<string, string[]>;
  size: Size;
}

const SUB_KEY = "sub-delegates";
const ROOT_KEY = "roots";

/**
 * Where cards sit: You, the architect, one column per delegate mode in the
 * order the modes first appeared, then sub-delegates. A pure function of the
 * topology and the start order, so a reload draws the same picture (ADR 0108).
 */
export function computeLayout(sessions: readonly BoardSession[], compact: boolean): Layout {
  const ordered = inStartOrder(sessions);
  const byId = new Map(ordered.map((s) => [s.id, s]));
  const architect = ordered.find((s) => s.role === "architect");
  const columnOf = (s: BoardSession): { key: string; label: string } => {
    const parent = byId.get(s.parentSessionId);
    if (!parent) return { key: ROOT_KEY, label: architect ? "Architect" : "Started by you" };
    if (parent.id === architect?.id) return { key: `mode:${s.mode}`, label: s.mode || "session" };
    return { key: SUB_KEY, label: "Sub-delegates" };
  };
  const order: string[] = [];
  const members = new Map<string, { label: string; ids: string[] }>();
  for (const s of ordered) {
    const { key, label } = columnOf(s);
    if (!members.has(key)) {
      members.set(key, { label, ids: [] });
      order.push(key);
    }
    members.get(key)!.ids.push(s.id);
  }
  // The roots first, the sub-delegates last, the modes in first-appearance order between.
  const keys = [...order.filter((k) => k === ROOT_KEY), ...order.filter((k) => k !== ROOT_KEY && k !== SUB_KEY), ...order.filter((k) => k === SUB_KEY)];

  const card = sizeOf(false, compact);
  const you = sizeOf(true, compact);
  const positions: Record<string, Point> = { [YOU_ID]: { x: MARGIN, y: MARGIN + HEADING } };
  const columns: Column[] = [];
  const slots: Record<string, string[]> = {};
  const gap = columnGap(compact);
  let x = MARGIN + you.w + gap;
  let bottom = MARGIN + HEADING + you.h;
  for (const key of keys) {
    const { label, ids } = members.get(key)!;
    columns.push({ key, label, count: ids.length, x, y: MARGIN, w: card.w });
    slots[key] = ids;
    ids.forEach((id, row) => {
      positions[id] = { x, y: MARGIN + HEADING + row * (card.h + ROW_GAP) };
    });
    bottom = Math.max(bottom, MARGIN + HEADING + ids.length * card.h + (ids.length - 1) * ROW_GAP);
    x += card.w + gap;
  }
  return { positions, columns, slots, size: { w: Math.max(x - gap + MARGIN, MARGIN * 2 + you.w), h: bottom + MARGIN } };
}

export interface Rect extends Point, Size {}

export interface EdgeGeometry {
  /** The SVG path from the right side of the source to the left side of the target. */
  d: string;
  /** Where a thread badge sits: on the line, in the room just before the target card, clear of the cards the line passes. */
  badge: Point;
}

/** A smooth path between two cards, with horizontal handles; the badge `room` px before the target. */
export function edgeGeometry(from: Rect, to: Rect, room: number): EdgeGeometry {
  const a = { x: from.x + from.w, y: from.y + from.h / 2 };
  const b = { x: to.x, y: to.y + to.h / 2 };
  const dx = Math.max(32, Math.abs(b.x - a.x) / 2);
  const c1 = { x: a.x + dx, y: a.y };
  const c2 = { x: b.x - dx, y: b.y };
  const at = (t: number, p0: number, p1: number, p2: number, p3: number) => {
    const u = 1 - t;
    return u * u * u * p0 + 3 * u * u * t * p1 + 3 * u * t * t * p2 + t * t * t * p3;
  };
  // The curve runs left to right, so x is monotonic in t: bisect for the point `room` before the target.
  const wanted = Math.max(a.x, b.x - room);
  let lo = 0;
  let hi = 1;
  for (let i = 0; i < 24; i++) {
    const mid = (lo + hi) / 2;
    if (at(mid, a.x, c1.x, c2.x, b.x) < wanted) lo = mid;
    else hi = mid;
  }
  return {
    d: `M${a.x},${a.y} C${c1.x},${c1.y} ${c2.x},${c2.y} ${b.x},${b.y}`,
    badge: { x: at(hi, a.x, c1.x, c2.x, b.x), y: at(hi, a.y, c1.y, c2.y, b.y) },
  };
}
