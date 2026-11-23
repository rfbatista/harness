// The presenter of the agents whiteboard (ADR 0108): the cards and edges of a
// task's sessions, as a view model. Framework-free; the view is passive.

import { relativeTime } from "../../../../shared/kernel/format";
import { Presenter } from "../../../../shared/kernel/presenter";
import { BoardSourceError, type BoardEvent, type BoardSource, type LiveStatus } from "../../application/ports";
import type { BoardPreferences } from "../../application/preferences";
import { centerOn, fit, isVisible, panBy, zoomAt, type Camera } from "../../domain/camera";
import { agentLabel, chipOf, detailOf, needsYou, statusView } from "../../domain/card";
import { startedEdges, YOU_ID } from "../../domain/graph";
import { MARGIN, computeLayout, edgeGeometry, sizeOf, type Point, type Rect } from "../../domain/layout";
import { upsertMessage, upsertReview, type ReviewRequest, type TaskMessage } from "../../domain/message";
import { freeSpot } from "../../domain/placement";
import { inStartOrder, isTerminal, upsertSession, type BoardSession } from "../../domain/session";
import { KIND_WORD, ageWords, lastReportOf, pairKey, pendingMessages, reviewWaits, threadsOf } from "../../domain/threads";
import { LOADING, type BadgeViewModel, type BoardViewModel, type CardViewModel, type EdgeViewModel } from "./board-view-model";

export type { BadgeViewModel, BoardViewModel, CardViewModel, EdgeViewModel } from "./board-view-model";

export interface BoardDeps {
  source: BoardSource;
  preferences: BoardPreferences;
  ticketId: string;
  projectId: string;
  agentNames: Readonly<Record<string, string>>;
  taskTitle: string;
  now: () => Date;
  /** Runs `fn` after `ms`; returns what cancels it. The ring and the pulse end through it. */
  schedule?: (fn: () => void, ms: number) => () => void;
}

const RING_MS = 2400;
const PULSE_MS = 1600;

const setTimer = (fn: () => void, ms: number): (() => void) => {
  const id = setTimeout(fn, ms);
  return () => clearTimeout(id);
};

/** What a person would notice about a session changing. */
const visibleState = (s: BoardSession): string => `${s.status}|${s.pendingApprovals}|${s.openInterviewCount}|${s.interruptedAt?.getTime() ?? ""}`;

const KIND_PHRASE: Record<string, string> = { review_request: "a review request", status_report: "a report", question: "a question", reply: "a reply" };

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
  #ringed = new Set<string>();
  #pulsing = new Set<string>();
  #announcement = "";
  #arrival: { id: string; text: string } | null = null;
  #feed: LiveStatus = "connecting";
  #stop: (() => void) | null = null;
  #stopped = false;
  readonly #cancels = new Set<() => void>();

  constructor(deps: BoardDeps) {
    super(LOADING);
    this.#deps = deps;
  }

  async start(): Promise<void> {
    try {
      await this.#read(false);
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

  /** Follows the project's live feed until `stop`. */
  follow(): void {
    if (this.#stopped) return;
    this.#stop ??= this.#deps.source.follow(this.#deps.projectId, {
      onEvent: (event) => this.#onEvent(event),
      onStatus: (status) => this.#onStatus(status),
    });
  }

  stop(): void {
    this.#stopped = true;
    this.#stop?.();
    this.#stop = null;
    for (const cancel of this.#cancels) cancel();
    this.#cancels.clear();
  }

  // ── camera ─────────────────────────────────────────────────────────────

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

  /** Pans to the card that arrived off-screen. */
  showArrival(): void {
    const arrival = this.#arrival;
    const rect = arrival && this.viewModel.cards.find((c) => c.id === arrival.id)?.rect;
    if (!rect) return;
    this.#moveCamera(centerOn(this.#camera, rect, this.#viewport));
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

  /** The pane opens or the page narrows: cards take their compact form. */
  setCompact(compact: boolean): void {
    if (compact === this.#compact) return;
    this.#compact = compact;
    if (this.viewModel.phase === "ready") this.set(this.#build());
  }

  // ── dragging ───────────────────────────────────────────────────────────

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

  // ── the live feed ──────────────────────────────────────────────────────

  /** Reads the task again. The first read just fills the board; a resync diffs against it. */
  async #read(resync: boolean): Promise<void> {
    const { source, ticketId } = this.#deps;
    const [sessions, messages, reviews] = await Promise.all([source.sessions(ticketId), source.messages(ticketId), source.pendingReviews(ticketId)]);
    if (!resync) {
      [this.#sessions, this.#messages, this.#reviews] = [sessions, messages, reviews];
      return;
    }
    // What changed rings, what is new arrives, nothing moves.
    const known = new Map(this.#sessions.map((s) => [s.id, s]));
    this.#sessions = sessions.filter((s) => known.has(s.id));
    for (const s of sessions) {
      const before = known.get(s.id);
      if (before) {
        this.#sessions = upsertSession(this.#sessions, s);
        if (visibleState(before) !== visibleState(s)) this.#ring(s.id);
      } else {
        this.#sessions = upsertSession(this.#sessions, s);
        this.#arrive(s, false);
      }
    }
    this.#messages = messages;
    this.#reviews = reviews;
  }

  #onStatus(status: LiveStatus): void {
    this.#feed = status;
    if (this.viewModel.phase === "ready") this.set(this.#build());
    if (status === "resynced") {
      void this.#read(true).then(
        () => this.set(this.#build()),
        (err: unknown) => {
          if (!(err instanceof BoardSourceError)) throw err;
        },
      );
    }
  }

  #onEvent(event: BoardEvent): void {
    if (this.viewModel.phase !== "ready") return;
    switch (event.kind) {
      case "session":
        if (event.session.ticketId === this.#deps.ticketId) this.#onSession(event.session);
        break;
      case "session-deleted":
        this.#sessions = this.#sessions.filter((s) => s.id !== event.id);
        break;
      case "message":
        if (event.message.taskId === this.#deps.ticketId) this.#onMessage(event.message);
        break;
      case "review":
        if (event.review.taskId === this.#deps.ticketId) this.#onReview(event.review);
        break;
    }
    this.set(this.#build());
  }

  #onSession(s: BoardSession): void {
    const before = this.#sessions.find((b) => b.id === s.id);
    this.#sessions = upsertSession(this.#sessions, s);
    const name = this.#nameOf(s.id);
    if (!before) {
      this.#arrive(s, true);
      return;
    }
    if (visibleState(before) === visibleState(s)) return;
    this.#ring(s.id);
    this.#announce(isTerminal(before) && !isTerminal(s) ? `${name} resumed` : `${name}: ${statusView(s).word}`);
  }

  /** A session joined the task: its computed slot, or the first free spot below when a placed card covers it. */
  #arrive(s: BoardSession, announce: boolean): void {
    const geometry = this.#geometry();
    const own = geometry.rects[s.id];
    if (own) {
      const taken = Object.entries(geometry.rects).filter(([id]) => id !== s.id).map(([, r]) => r);
      const spot = freeSpot(own, taken);
      if (spot.y !== own.y) {
        this.#placed = { ...this.#placed, [s.id]: spot };
        this.#deps.preferences.savePositions(this.#deps.ticketId, this.#placed);
      }
    }
    this.#ring(s.id);
    const name = this.#nameOf(s.id);
    if (announce) this.#announce(`${name} started`);
    const rect = this.#geometry().rects[s.id];
    if (announce && rect && this.#viewport.w > 0 && !isVisible(this.#camera, rect, this.#viewport)) this.#arrival = { id: s.id, text: `${name} arrived · Show` };
  }

  #onMessage(m: TaskMessage): void {
    const fresh = !this.#messages.some((known) => known.id === m.id);
    this.#messages = upsertMessage(this.#messages, m);
    // The server sends a message again once it is delivered: that is not news.
    if (!fresh) return;
    const edge = startedEdges(this.#sessions).find((e) => pairKey(e.from, e.to) === pairKey(m.fromSessionId, m.toSessionId));
    if (edge) this.#pulse(edge.id);
    this.#announce(`${this.#nameOf(m.fromSessionId)} sent ${this.#nameOf(m.toSessionId)} ${KIND_PHRASE[m.kind] ?? "a message"}`);
  }

  #onReview(r: ReviewRequest): void {
    const known = this.#reviews.find((k) => k.id === r.id);
    this.#reviews = upsertReview(this.#reviews, r);
    if (r.state !== "pending" || known?.state === "pending") return;
    this.#pulse(`${YOU_ID}>${r.architectSessionId}`);
    this.#announce("A review request waits on you");
  }

  #nameOf(id: string): string {
    return agentLabel(this.#sessions.find((s) => s.id === id)?.agentId ?? "", this.#deps.agentNames);
  }

  #announce(text: string): void {
    this.#announcement = text;
  }

  #ring(id: string): void {
    this.#ringed.add(id);
    this.#later(() => {
      this.#ringed.delete(id);
      this.set(this.#build());
    }, RING_MS);
  }

  #pulse(edgeId: string): void {
    this.#pulsing.add(edgeId);
    this.#later(() => {
      this.#pulsing.delete(edgeId);
      this.set(this.#build());
    }, PULSE_MS);
  }

  #later(fn: () => void, ms: number): void {
    const cancel = (this.#deps.schedule ?? setTimer)(() => {
      this.#cancels.delete(cancel);
      fn();
    }, ms);
    this.#cancels.add(cancel);
  }

  // ── the view model ─────────────────────────────────────────────────────

  /** Where every card sits: the computed layout, with the person's places over it. */
  #geometry(): { layout: ReturnType<typeof computeLayout>; rects: Record<string, Rect>; placed: Record<string, Point>; canvas: { w: number; h: number } } {
    const layout = computeLayout(this.#sessions, this.#compact);
    const known = new Set(this.#sessions.map((s) => s.id));
    const placed = Object.fromEntries(Object.entries(this.#placed).filter(([id]) => known.has(id)));
    const rects: Record<string, Rect> = {};
    for (const [id, p] of Object.entries({ ...layout.positions, ...placed })) rects[id] = { ...p, ...sizeOf(id === YOU_ID, this.#compact) };
    const canvas = {
      w: Math.max(layout.size.w, ...Object.values(rects).map((r) => r.x + r.w + MARGIN)),
      h: Math.max(layout.size.h, ...Object.values(rects).map((r) => r.y + r.h + MARGIN)),
    };
    return { layout, rects, placed, canvas };
  }

  #build(): BoardViewModel {
    const sessions = this.#sessions;
    const compact = this.#compact;
    const { layout, rects, placed, canvas } = this.#geometry();
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
    const nameOf = (id: string) => (id === YOU_ID ? "You" : this.#nameOf(id));
    const edges = startedEdges(sessions).map((e): EdgeViewModel => {
      const geometry = edgeGeometry(rects[e.from]!, rects[e.to]!);
      const base = { id: e.id, from: e.from, to: e.to, d: geometry.d, dimmed: ended.has(e.to), pulsing: this.#pulsing.has(e.id) };
      const wait = e.from === YOU_ID ? waits.get(e.to) : undefined;
      if (wait) {
        const badge: BadgeViewModel = {
          count: wait.count,
          text: `review request · waits ${relativeTime(wait.oldest, now)}`,
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
    const arrivalRect = this.#arrival && rects[this.#arrival.id];
    if (this.#arrival && (!arrivalRect || (this.#viewport.w > 0 && isVisible(this.#camera, arrivalRect, this.#viewport)))) this.#arrival = null;
    const placedCount = Object.keys(placed).length;
    return {
      phase: "ready",
      error: "",
      compact,
      placedCount,
      tidyEnabled: placedCount > 0,
      camera: this.#camera,
      zoomPercent: Math.round(this.#camera.scale * 100),
      columns: layout.columns,
      canvas,
      cards,
      edges,
      feed: this.#feed,
      announcement: this.#announcement,
      arrival: this.#arrival,
    };
  }

  #you(rect: Rect, waiting: number): CardViewModel {
    return {
      id: YOU_ID, kind: "you", rect, agent: "You", mode: "", model: "", branch: "", badge: "", state: waiting > 0 ? "waiting" : "idle",
      word: waiting > 0 ? "waiting on you" : "nothing waiting", chip: "", line: null, dimmed: false, tone: "", dragging: false, ringed: false, when: "", attention: waiting > 0,
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
      ringed: this.#ringed.has(s.id),
      when: relativeTime(s.updatedAt, this.#deps.now()),
      attention: needsYou(s),
      name: [agentLabel(s.agentId, agentNames), s.role === "architect" ? "architect" : s.mode, status.word, pendingKind ? `${pendingKind} waiting on the architect` : "", isTerminal(s) && s.status !== "failed" ? "ended" : ""].filter(Boolean).join(", "),
    };
  }
}
