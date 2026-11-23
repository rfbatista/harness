// The presenter of the agents whiteboard (ADR 0108): the cards and edges of a
// task's sessions, as a view model. Framework-free; the view is passive.

import { relativeTime } from "../../../../shared/kernel/format";
import { Presenter } from "../../../../shared/kernel/presenter";
import { BoardSourceError, type BoardEvent, type BoardSource, type LiveStatus } from "../../application/ports";
import { noHost, type BoardHost } from "../../application/host";
import type { PageSignals } from "../../application/page-bridge";
import type { BoardPreferences } from "../../application/preferences";
import { centerOn, fit, isVisible, panBy, zoomAt, type Camera } from "../../domain/camera";
import { agentLabel, chipOf, detailOf, needsYou, statusView } from "../../domain/card";
import { startedEdges, YOU_ID } from "../../domain/graph";
import { MARGIN, computeLayout, edgeGeometry, sizeOf, type Point, type Rect } from "../../domain/layout";
import { pushStateOf, unapprovedCount, upsertBranches, type SessionBranches } from "../../domain/branches";
import { upsertMessage, upsertReview, type ReviewRequest, type TaskMessage } from "../../domain/message";
import { freeSpot } from "../../domain/placement";
import { allEnded } from "../../domain/graph";
import { isTerminal, upsertSession, type BoardSession } from "../../domain/session";
import { KIND_WORD, ageWords, lastReportOf, pairKey, pendingMessages, reviewWaits, threadsOf } from "../../domain/threads";
import { LOADING, type BadgeViewModel, type BoardViewModel, type CardViewModel, type EdgeViewModel } from "./board-view-model";

export type { BadgeViewModel, BoardViewModel, CardViewModel, EdgeViewModel } from "./board-view-model";

export interface BoardDeps {
  source: BoardSource;
  preferences: BoardPreferences;
  /** The page the board sits in; none: the board asks nothing of it. */
  host?: BoardHost;
  /** What the page tells the board: the session it selected. */
  signals?: PageSignals;
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
/** A pan or zoom is kept once the camera has been still this long. */
const CAMERA_SAVE_MS = 250;

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
  #branches: SessionBranches[] = [];
  /** The page is narrower than 64rem. */
  #narrow = false;
  #selectedId: string | null = null;
  #focusedId: string | null = null;
  /** The positions the person gave, by session id; kept in the browser per task. */
  #placed: Record<string, Point> = {};
  /** Cards a new session would have moved, held where they were drawn until Tidy; not kept in the browser. */
  #pinned: Record<string, Point> = {};
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
  #cancelCameraSave: (() => void) | null = null;

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
    if (this.#stopped || this.#stop) return;
    const feed = this.#deps.source.follow(this.#deps.projectId, {
      onEvent: (event) => this.#onEvent(event),
      onStatus: (status) => this.#onStatus(status),
    });
    const page = this.#deps.signals?.followSelection((id) => this.selectFromPage(id));
    this.#stop = () => {
      feed();
      page?.();
    };
    // The page answers with the session it has selected.
    this.#host.boardReady();
  }

  stop(): void {
    this.#stopped = true;
    // A camera still moving when the board goes away is kept as it was.
    if (this.#cancelCameraSave) {
      this.#cancelCameraSave();
      this.#cancelCameraSave = null;
      this.#deps.preferences.saveCamera(this.#deps.ticketId, this.#camera);
    }
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
    this.#saveCameraSoon();
    this.set(this.#build());
  }

  /** Keeps the camera in the browser once it has stopped moving: a drag or a wheel is many steps. */
  #saveCameraSoon(): void {
    this.#cancelCameraSave?.();
    this.#cancelCameraSave = (this.#deps.schedule ?? setTimer)(() => {
      this.#cancelCameraSave = null;
      this.#deps.preferences.saveCamera(this.#deps.ticketId, this.#camera);
    }, CAMERA_SAVE_MS);
  }

  /** The page is narrower than 64rem: cards take their compact form. */
  setNarrow(narrow: boolean): void {
    if (narrow === this.#narrow) return;
    this.#narrow = narrow;
    if (this.viewModel.phase === "ready") this.set(this.#build());
  }

  /** Cards are compact while the pane is open beside the board, or on a narrow page. */
  get #compact(): boolean {
    return this.#narrow || this.#selectedId !== null;
  }

  // ── selection and keys ─────────────────────────────────────────────────

  get #host(): BoardHost {
    return this.#deps.host ?? noHost;
  }

  /** The person picks a card: its pane opens beside the board. */
  select(id: string, tab?: "context" | "conversation"): void {
    if (id === YOU_ID || !this.#sessions.some((s) => s.id === id)) return;
    this.#selectedId = id;
    this.#focusedId = id;
    this.#host.openSession(id, tab);
    this.set(this.#build());
  }

  /** The page selected a session itself (the list, the URL, the pane's Back): the board shows it. */
  selectFromPage(id: string | null): void {
    this.#selectedId = id !== null && this.#sessions.some((s) => s.id === id) ? id : null;
    if (this.viewModel.phase === "ready") this.set(this.#build());
  }

  focus(id: string | null): void {
    if (id === this.#focusedId) return;
    this.#focusedId = id;
    const rect = id && this.viewModel.cards.find((c) => c.id === id)?.rect;
    // The focused card is panned into view, never zoomed.
    if (rect && this.#viewport.w > 0 && !isVisible(this.#camera, rect, this.#viewport)) this.#moveCamera(centerOn(this.#camera, rect, this.#viewport));
    else this.set(this.#build());
  }

  /** A click or Enter on a card: a session opens its pane; You opens the Reviews popover while a review waits. */
  open(id: string): void {
    if (id !== YOU_ID) this.select(id);
    else if (this.viewModel.cards[0]?.attention) this.#host.openReviews();
  }

  /** A message pill: the You pill opens Reviews, any other the delegate's Conversation tab. */
  openThread(edgeId: string): void {
    const edge = startedEdges(this.#sessions).find((e) => e.id === edgeId);
    if (!edge) return;
    if (edge.from === YOU_ID) this.#host.openReviews();
    else this.select(edge.to, "conversation");
  }

  newSession(): void {
    this.#host.newSession();
  }

  showList(): void {
    this.#host.showList();
  }

  /** A key on the canvas. Returns whether the board used it. */
  key({ key }: { key: string }): boolean {
    const focused = this.#focusedId;
    if (key === "Escape") {
      if (this.#selectedId !== null) this.#host.closePane();
      this.#selectedId = null;
      this.#focusedId = null;
      this.set(this.#build());
      return true;
    }
    if (focused === null) {
      const step = 40;
      const pan: Record<string, [number, number]> = { ArrowLeft: [step, 0], ArrowRight: [-step, 0], ArrowUp: [0, step], ArrowDown: [0, -step] };
      const by = pan[key];
      if (!by) return false;
      this.panBy(...by);
      return true;
    }
    if (key === "Enter") {
      this.open(focused);
      return true;
    }
    if (key.toLowerCase() === "c") {
      if (focused !== YOU_ID) this.select(focused, "context");
      return true;
    }
    const next = this.#neighbour(focused, key);
    if (next === undefined) return false;
    this.focus(next);
    return true;
  }

  /** Where an arrow key leads from `id`: the card below or above in its column, its first child, or its parent; itself at an end. */
  #neighbour(id: string, key: string): string | undefined {
    const { layout } = this.#geometry();
    const edges = startedEdges(this.#sessions);
    if (key === "ArrowDown" || key === "ArrowUp") {
      const column = layout.columns.map((c) => layout.slots[c.key]!).find((ids) => ids.includes(id));
      if (!column) return id;
      return column[column.indexOf(id) + (key === "ArrowDown" ? 1 : -1)] ?? id;
    }
    if (key === "ArrowRight") return this.#rank().find((r) => edges.some((e) => e.from === id && e.to === r)) ?? id;
    if (key === "ArrowLeft") return edges.find((e) => e.to === id)?.from ?? id;
    return undefined;
  }

  /** You, the architect, then column by column, top to bottom. */
  #rank(): string[] {
    const { layout } = this.#geometry();
    return [YOU_ID, ...layout.columns.flatMap((c) => layout.slots[c.key]!)];
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
    this.#pinned = {};
    this.#deps.preferences.savePositions(this.#deps.ticketId, {});
    this.set(this.#build());
  }

  // ── the live feed ──────────────────────────────────────────────────────

  /** Reads the task again. The first read just fills the board; a resync diffs against it. */
  async #read(resync: boolean): Promise<void> {
    const { source, ticketId } = this.#deps;
    // A card can do without its push state: a Branches list that does not read leaves it out.
    const branchLists = source.branches(ticketId).catch((err: unknown) => {
      if (err instanceof BoardSourceError) return [];
      throw err;
    });
    const [sessions, messages, reviews, branches] = await Promise.all([source.sessions(ticketId), source.messages(ticketId), source.pendingReviews(ticketId), branchLists]);
    this.#branches = branches;
    if (!resync) {
      [this.#sessions, this.#messages, this.#reviews] = [sessions, messages, reviews];
      return;
    }
    // What changed rings, what is new arrives, nothing moves.
    const drawn = this.#geometry().rects;
    const known = new Map(this.#sessions.map((s) => [s.id, s]));
    this.#sessions = sessions;
    this.#pinWhatMoved(drawn);
    for (const s of sessions) {
      const before = known.get(s.id);
      if (!before) this.#arrive(s, false);
      else if (visibleState(before) !== visibleState(s)) this.#ring(s.id);
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
      case "session-deleted": {
        const drawn = this.#geometry().rects;
        this.#sessions = this.#sessions.filter((s) => s.id !== event.id);
        this.#pinWhatMoved(drawn);
        break;
      }
      case "message":
        if (event.message.taskId === this.#deps.ticketId) this.#onMessage(event.message);
        break;
      case "review":
        if (event.review.taskId === this.#deps.ticketId) this.#onReview(event.review);
        break;
      case "branches":
        if (this.#sessions.some((s) => s.id === event.branches.sessionId)) this.#branches = upsertBranches(this.#branches, event.branches);
        break;
    }
    this.set(this.#build());
  }

  #onSession(s: BoardSession): void {
    const before = this.#sessions.find((b) => b.id === s.id);
    const drawn = before ? null : this.#geometry().rects;
    this.#sessions = upsertSession(this.#sessions, s);
    if (drawn) this.#pinWhatMoved(drawn);
    const name = this.#nameOf(s.id);
    if (!before) {
      this.#arrive(s, true);
      return;
    }
    if (visibleState(before) === visibleState(s)) return;
    this.#ring(s.id);
    this.#announce(isTerminal(before) && !isTerminal(s) ? `${name} resumed` : `${name}: ${statusView(s).word}`);
  }

  /** A new session can open a column and shift the ones after it: no card moves on its own, so those stay where they were drawn. */
  #pinWhatMoved(drawn: Record<string, Rect>): void {
    const now = this.#geometry().rects;
    for (const [id, was] of Object.entries(drawn)) {
      const at = now[id];
      if (at && (at.x !== was.x || at.y !== was.y)) this.#pinned = { ...this.#pinned, [id]: { x: was.x, y: was.y } };
    }
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
    const pinned = Object.fromEntries(Object.entries(this.#pinned).filter(([id]) => known.has(id)));
    const rects: Record<string, Rect> = {};
    for (const [id, p] of Object.entries({ ...layout.positions, ...pinned, ...placed })) rects[id] = { ...p, ...sizeOf(id === YOU_ID, this.#compact) };
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
    const byId = new Map(sessions.map((s) => [s.id, s]));
    const cards: CardViewModel[] = this.#rank().map((id) =>
      id === YOU_ID ? this.#you(rects[YOU_ID]!, reviewsWaiting) : this.#card(byId.get(id)!, rects[id]!, waitingOnArchitect.get(id) ?? ""),
    );
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
      selectedId: this.#selectedId,
      focusedId: this.#focusedId,
      state: sessions.length === 0 ? "empty" : allEnded(sessions) ? "ended" : "live",
      endedNote: allEnded(sessions) ? `All ${sessions.length} sessions ended · the picture stays as it was` : "",
      canvasLabel: `${sessions.length} session${sessions.length === 1 ? "" : "s"} as a graph. Tab moves between agents, arrows walk the tree, Enter opens a session, C opens its Context tab.`,
    };
  }

  #you(rect: Rect, waiting: number): CardViewModel {
    return {
      id: YOU_ID, kind: "you", rect, agent: "You", mode: "", model: "", branch: "", pushState: "", unapproved: 0, badge: "", state: waiting > 0 ? "waiting" : "idle",
      word: waiting > 0 ? "waiting on you" : "nothing waiting", chip: "", line: null, dimmed: false, tone: "", dragging: false, ringed: false, when: "", attention: waiting > 0,
      name: waiting > 0 ? `You, ${waiting} review request${waiting === 1 ? "" : "s"} waiting on you` : "You, nothing waiting on you",
      selected: false,
      focused: this.#focusedId === YOU_ID,
    };
  }

  #card(s: BoardSession, rect: Rect, pendingKind: string): CardViewModel {
    const status = statusView(s);
    const { agentNames, taskTitle } = this.#deps;
    const branches = this.#branches.find((b) => b.sessionId === s.id);
    const ended = isTerminal(s) && s.status !== "failed";
    return {
      id: s.id,
      kind: "session",
      rect,
      agent: agentLabel(s.agentId, agentNames),
      mode: s.role === "architect" ? "" : s.mode,
      model: s.model,
      branch: s.branch,
      pushState: pushStateOf(branches),
      unapproved: unapprovedCount(branches),
      badge: s.role === "architect" ? "architect" : "",
      state: status.state,
      word: status.word,
      chip: chipOf(status),
      line: detailOf(s, { taskTitle, agentNames, others: this.#sessions, report: lastReportOf(this.#messages, s.id), messagesLoaded: true }),
      dimmed: ended,
      tone: s.status === "failed" ? "danger" : "",
      dragging: this.#drag?.id === s.id,
      ringed: this.#ringed.has(s.id),
      when: relativeTime(s.updatedAt, this.#deps.now()),
      attention: needsYou(s),
      name: [agentLabel(s.agentId, agentNames), s.role === "architect" ? "architect" : s.mode, status.word, pendingKind ? `${pendingKind} waiting on the architect` : "", ended ? "ended" : ""].filter(Boolean).join(", "),
      selected: this.#selectedId === s.id,
      focused: this.#focusedId === s.id,
    };
  }
}
