// The presenter of the agents whiteboard (ADR 0108): the cards and edges of a
// task's sessions, as a view model. Framework-free; the view is passive.

import { Presenter } from "../../../../shared/kernel/presenter";
import { BoardSourceError, type BoardSource } from "../../application/ports";
import type { BoardPreferences } from "../../application/preferences";
import { agentLabel, chipOf, detailOf, statusView, type Detail } from "../../domain/card";
import { startedEdges, YOU_ID } from "../../domain/graph";
import { fit, panBy, zoomAt, type Camera } from "../../domain/camera";
import { MARGIN, computeLayout, edgeGeometry, sizeOf, type Point, type Rect } from "../../domain/layout";
import { inStartOrder, isTerminal, type BoardSession } from "../../domain/session";
import type { ReviewRequest, TaskMessage } from "../../domain/message";
import { KIND_WORD, ageWords, lastReportOf, pairKey, pendingMessages, reviewWaits, threadsOf } from "../../domain/threads";
import { needsYou } from "../../domain/card";
import { relativeTime } from "../../../../shared/kernel/format";

export interface CardViewModel {
  id: string;
  kind: "you" | "session";
  rect: Rect;
  agent: string;
  mode: string;
  model: string;
  branch: string;
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
  when: string;
  /** Waiting on the person: the card takes the attention wash. */
  attention: boolean;
  /** The accessible name, which reads like the row's tooltip. */
  name: string;
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
}

export interface BoardDeps {
  source: BoardSource;
  preferences: BoardPreferences;
  ticketId: string;
  projectId: string;
  agentNames: Readonly<Record<string, string>>;
  taskTitle: string;
  now: () => Date;
}

const LOADING: BoardViewModel = { phase: "loading", error: "", compact: false, placedCount: 0, tidyEnabled: false, camera: { x: 0, y: 0, scale: 1 }, zoomPercent: 100, columns: [], canvas: { w: 0, h: 0 }, cards: [], edges: [] };

export class BoardPresenter extends Presenter<BoardViewModel> {
  readonly #deps: BoardDeps;
  #sessions: BoardSession[] = [];
  #messages: TaskMessage[] = [];
  #reviews: ReviewRequest[] = [];
  #compact = false;
  /** The positions the person gave, by session id; kept in the browser per task. */
  #placed: Record<string, Point> = {};
  #drag: { id: string; from: Point } | null = null;
  #camera: Camera = { x: 0, y: 0, scale: 1 };
  /** The person has not moved the camera (and none was stored): the board is framed whole once it has a viewport. */
  #framed = false;
  #viewport = { w: 0, h: 0 };

  constructor(deps: BoardDeps) {
    super(LOADING);
    this.#deps = deps;
  }

  async start(): Promise<void> {
    try {
      const { source, ticketId } = this.#deps;
      [this.#sessions, this.#messages, this.#reviews] = await Promise.all([source.sessions(ticketId), source.messages(ticketId), source.pendingReviews(ticketId)]);
    } catch (err) {
      if (!(err instanceof BoardSourceError)) throw err;
      this.set({ ...LOADING, phase: "error", error: err.message });
      return;
    }
    this.#placed = this.#deps.preferences.positions(this.#deps.ticketId);
    const stored = this.#deps.preferences.camera(this.#deps.ticketId);
    if (stored) {
      this.#camera = stored;
      this.#framed = true;
    }
    this.set(this.#build());
    this.#frame();
  }

  /** The canvas has this size in px. */
  setViewport(w: number, h: number): void {
    this.#viewport = { w, h };
    this.#frame();
  }

  panBy(dx: number, dy: number): void {
    this.#moveCamera(panBy(this.#camera, dx, dy));
  }

  /** Zoom by `factor` around the viewport point (x, y). */
  zoomAt(factor: number, x: number, y: number): void {
    this.#moveCamera(zoomAt(this.#camera, factor, { x, y }));
  }

  /** Zoom by `factor` around the middle of the viewport. */
  zoomBy(factor: number): void {
    this.zoomAt(factor, this.#viewport.w / 2, this.#viewport.h / 2);
  }

  fit(): void {
    if (this.#viewport.w > 0) this.#moveCamera(fit(this.viewModel.canvas, this.#viewport));
  }

  actualSize(): void {
    this.zoomAt(1 / this.#camera.scale, this.#viewport.w / 2, this.#viewport.h / 2);
  }

  /** Frames the board whole, once, unless the person's camera was restored. */
  #frame(): void {
    if (this.#framed || this.viewModel.phase !== "ready" || this.#viewport.w <= 0) return;
    this.#framed = true;
    this.#camera = fit(this.viewModel.canvas, this.#viewport);
    this.set(this.#build());
  }

  #moveCamera(camera: Camera): void {
    this.#camera = camera;
    this.#framed = true;
    this.#deps.preferences.saveCamera(this.#deps.ticketId, camera);
    this.set(this.#build());
  }

  /** The person takes hold of a card; the You card is not movable. */
  beginDrag(id: string): void {
    const card = this.viewModel.cards.find((c) => c.id === id);
    if (!card || card.kind === "you") return;
    this.#drag = { id, from: { x: card.rect.x, y: card.rect.y } };
    this.set(this.#build());
  }

  /** The pointer has travelled (dx, dy) screen px since the grab. */
  dragTo(id: string, dx: number, dy: number): void {
    const drag = this.#drag;
    if (!drag || drag.id !== id) return;
    this.#placed = { ...this.#placed, [id]: { x: Math.max(0, drag.from.x + dx / this.#camera.scale), y: Math.max(0, drag.from.y + dy / this.#camera.scale) } };
    this.set(this.#build());
  }

  endDrag(id: string): void {
    if (this.#drag?.id !== id) return;
    this.#drag = null;
    this.#deps.preferences.savePositions(this.#deps.ticketId, this.#placed);
    this.set(this.#build());
  }

  /** Drops every placed position: the computed layout again. */
  tidy(): void {
    this.#placed = {};
    this.#deps.preferences.savePositions(this.#deps.ticketId, {});
    this.set(this.#build());
  }

  /** The pane opens or the page narrows: cards take their compact form. */
  setCompact(compact: boolean): void {
    if (compact === this.#compact) return;
    this.#compact = compact;
    if (this.viewModel.phase === "ready") this.set(this.#build());
  }

  #build(): BoardViewModel {
    const sessions = this.#sessions;
    const compact = this.#compact;
    const layout = computeLayout(sessions, compact);
    const known = new Set(sessions.map((s) => s.id));
    const placed = Object.fromEntries(Object.entries(this.#placed).filter(([id]) => known.has(id)));
    const rects: Record<string, Rect> = {};
    for (const [id, p] of Object.entries({ ...layout.positions, ...placed })) rects[id] = { ...p, ...sizeOf(id === YOU_ID, compact) };
    const canvas = {
      w: Math.max(layout.size.w, ...Object.values(rects).map((r) => r.x + r.w + MARGIN)),
      h: Math.max(layout.size.h, ...Object.values(rects).map((r) => r.y + r.h + MARGIN)),
    };
    const ended = new Set(sessions.filter(isTerminal).map((s) => s.id));
    const threads = threadsOf(this.#messages);
    const waits = reviewWaits(this.#reviews);
    const now = this.#deps.now();
    const architectId = sessions.find((s) => s.role === "architect")?.id ?? "";
    // What waits on the architect: a question or review request sent to it with no reply yet.
    const waitingOnArchitect = new Map<string, string>();
    for (const m of pendingMessages(this.#messages)) if (m.toSessionId === architectId && !waitingOnArchitect.has(m.fromSessionId)) waitingOnArchitect.set(m.fromSessionId, KIND_WORD[m.kind]);
    const reviewsWaiting = [...waits.values()].reduce((n, w) => n + w.count, 0);
    const cards: CardViewModel[] = [
      this.#you(rects[YOU_ID]!, reviewsWaiting),
      ...inStartOrder(sessions).map((s) => this.#card(s, rects[s.id]!, waitingOnArchitect.get(s.id) ?? "")),
    ];
    const nameOf = (id: string) => (id === YOU_ID ? "You" : agentLabel(sessions.find((s) => s.id === id)?.agentId ?? "", this.#deps.agentNames));
    const edges = startedEdges(sessions).map((e): EdgeViewModel => {
      const geometry = edgeGeometry(rects[e.from]!, rects[e.to]!);
      const base = { id: e.id, from: e.from, to: e.to, d: geometry.d, dimmed: ended.has(e.to) };
      const wait = e.from === YOU_ID ? waits.get(e.to) : undefined;
      if (wait) {
        const age = relativeTime(wait.oldest, now);
        const badge: BadgeViewModel = {
          count: wait.count,
          text: `review request · waits ${age}`,
          name: `${wait.count} review request${wait.count === 1 ? "" : "s"} ${wait.count === 1 ? "waits" : "wait"} on you, ${ageWords(wait.oldest, now)}`,
          tone: "attention",
          ...geometry.badge,
        };
        return { ...base, tone: "amber", badge };
      }
      const thread = e.from === YOU_ID ? undefined : threads.get(pairKey(e.from, e.to));
      if (!thread?.latest) return { ...base, tone: "started", badge: null };
      const waiting = thread.pending;
      const latest = thread.latest;
      const between = `${thread.count} message${thread.count === 1 ? "" : "s"} between ${nameOf(e.from)} and ${nameOf(e.to)}`;
      const badge: BadgeViewModel = waiting
        ? { count: thread.count, text: `${KIND_WORD[waiting.kind]} · waits ${relativeTime(waiting.createdAt, now)}`, name: `${between}, ${KIND_WORD[waiting.kind]} waits ${ageWords(waiting.createdAt, now)}`, tone: "signal", ...geometry.badge }
        : { count: thread.count, text: `${KIND_WORD[latest.kind]} ${relativeTime(latest.createdAt, now)}`, name: `${between}, latest ${KIND_WORD[latest.kind]} ${ageWords(latest.createdAt, now)} ago`, tone: "", ...geometry.badge };
      return { ...base, tone: waiting ? "signal" : "started", badge };
    });
    const placedCount = Object.keys(placed).length;
    return { phase: "ready", error: "", compact, placedCount, tidyEnabled: placedCount > 0, camera: this.#camera, zoomPercent: Math.round(this.#camera.scale * 100), columns: layout.columns, canvas, cards, edges };
  }

  #you(rect: Rect, waiting: number): CardViewModel {
    return {
      id: YOU_ID, kind: "you", rect, agent: "You", mode: "", model: "", branch: "", badge: "", state: waiting > 0 ? "waiting" : "idle",
      word: waiting > 0 ? "waiting on you" : "nothing waiting", chip: "", line: null, dimmed: false, tone: "", dragging: false, when: "", attention: waiting > 0,
      name: waiting > 0 ? `You, ${waiting} review request${waiting === 1 ? "" : "s"} waiting on you` : "You, nothing waiting on you",
    };
  }

  #card(s: BoardSession, rect: Rect, pendingKind: string): CardViewModel {
    const status = statusView(s);
    const { agentNames, taskTitle } = this.#deps;
    return {
      id: s.id,
      kind: "session",
      rect,
      agent: agentLabel(s.agentId, agentNames),
      mode: s.role === "architect" ? "" : s.mode,
      model: s.model,
      branch: s.branch,
      badge: s.role === "architect" ? "architect" : "",
      state: status.state,
      word: status.word,
      chip: chipOf(status),
      line: detailOf(s, { taskTitle, agentNames, others: this.#sessions, report: lastReportOf(this.#messages, s.id), messagesLoaded: true }),
      dimmed: isTerminal(s) && s.status !== "failed",
      tone: s.status === "failed" ? "danger" : "",
      dragging: this.#drag?.id === s.id,
      when: relativeTime(s.updatedAt, this.#deps.now()),
      attention: needsYou(s),
      name: [agentLabel(s.agentId, agentNames), s.role === "architect" ? "architect" : s.mode, status.word, pendingKind ? `${pendingKind} waiting on the architect` : "", isTerminal(s) && s.status !== "failed" ? "ended" : ""].filter(Boolean).join(", "),
    };
  }
}
