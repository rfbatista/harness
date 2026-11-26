// What the board's view renders: plain values, no behaviour.

import type { Camera } from "../../domain/camera";
import type { Detail } from "../../domain/card";
import type { Rect } from "../../domain/layout";
import type { LiveStatus } from "../../application/ports";
import type { CardMemory } from "./board-memory";

export interface CardViewModel {
  id: string;
  kind: "you" | "session";
  rect: Rect;
  agent: string;
  mode: string;
  model: string;
  branch: string;
  /** Where the session's own branch stands: "pushed", "not pushed", or "" when it has none. */
  pushState: "" | "pushed" | "not pushed";
  /** Branches nobody approved: the row flags them. */
  unapproved: number;
  /** "architect" on the architect's card. */
  badge: string;
  state: string;
  word: string;
  chip: string;
  line: Detail | null;
  dimmed: boolean;
  tone: "" | "danger";
  /** The person is dragging it. */
  dragging: boolean;
  /** One ring after it arrived or changed. */
  ringed: boolean;
  /** The memory line: null when the card says nothing about memory. */
  memory: CardMemory | null;
  /** One ring, in danger, after a process of it was ended. */
  memoryRinged: boolean;
  when: string;
  /** Waiting on the person: the card takes the attention wash. */
  attention: boolean;
  /** The accessible name, which reads like the row's tooltip. */
  name: string;
  selected: boolean;
  focused: boolean;
}

export interface BadgeViewModel {
  count: number;
  /** The latest message and its age, or what waits: "review request 11m", "question · waits 4m". */
  text: string;
  /** The full sentence, for the pill's accessible name. */
  name: string;
  tone: "" | "signal" | "attention";
  x: number;
  y: number;
}

export interface EdgeViewModel {
  id: string;
  from: string;
  to: string;
  d: string;
  dimmed: boolean;
  /** "started": grey; "signal": a message waits on the architect; "amber": a review request waits on the person. */
  tone: "started" | "signal" | "amber";
  /** One run of the pulse after a message landed. */
  pulsing: boolean;
  badge: BadgeViewModel | null;
}

export interface ColumnViewModel {
  key: string;
  label: string;
  count: number;
  x: number;
  y: number;
  w: number;
}

/** A card that arrived outside the visible area. */
export interface ArrivalViewModel {
  id: string;
  text: string;
}

export interface BoardViewModel {
  phase: "loading" | "ready" | "error";
  error: string;
  /** Compact cards: the pane is open, or the page is narrow. */
  compact: boolean;
  columns: ColumnViewModel[];
  /** How many cards the person placed, and whether Tidy has anything to do. */
  placedCount: number;
  tidyEnabled: boolean;
  camera: Camera;
  /** The zoom as a whole percentage. */
  zoomPercent: number;
  /** The extent of the board in board px. */
  canvas: { w: number; h: number };
  cards: CardViewModel[];
  edges: EdgeViewModel[];
  /** The live feed's state, and the last thing that changed, in one sentence (the live region reads it). */
  feed: LiveStatus;
  announcement: string;
  arrival: ArrivalViewModel | null;
  /** The session whose pane is open beside the board. */
  selectedId: string | null;
  /** The card with keyboard focus; null: the canvas itself. */
  focusedId: string | null;
  /** "empty": no session yet; "ended": every session has ended; otherwise "live". */
  state: "empty" | "ended" | "live";
  endedNote: string;
  canvasLabel: string;
}

export const LOADING: BoardViewModel = {
  phase: "loading",
  error: "",
  compact: false,
  columns: [],
  placedCount: 0,
  tidyEnabled: false,
  camera: { x: 0, y: 0, scale: 1 },
  zoomPercent: 100,
  canvas: { w: 0, h: 0 },
  cards: [],
  edges: [],
  feed: "connecting",
  announcement: "",
  arrival: null,
  selectedId: null,
  focusedId: null,
  state: "empty",
  endedNote: "",
  canvasLabel: "",
};
