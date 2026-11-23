// The presenter of the agents whiteboard (ADR 0108): the cards and edges of a
// task's sessions, as a view model. Framework-free; the view is passive.

import { Presenter } from "../../../../shared/kernel/presenter";
import { BoardSourceError, type BoardSource } from "../../application/ports";
import { agentLabel, chipOf, detailOf, statusView, type Detail } from "../../domain/card";
import { startedEdges, YOU_ID } from "../../domain/graph";
import { computeLayout, edgeGeometry, sizeOf, type Rect } from "../../domain/layout";
import { inStartOrder, isTerminal, type BoardSession } from "../../domain/session";
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
  when: string;
  /** Waiting on the person: the card takes the attention wash. */
  attention: boolean;
  /** The accessible name, which reads like the row's tooltip. */
  name: string;
}

export interface EdgeViewModel {
  id: string;
  from: string;
  to: string;
  d: string;
  dimmed: boolean;
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
  /** The extent of the board in board px. */
  canvas: { w: number; h: number };
  cards: CardViewModel[];
  edges: EdgeViewModel[];
}

export interface BoardDeps {
  source: BoardSource;
  ticketId: string;
  projectId: string;
  agentNames: Readonly<Record<string, string>>;
  taskTitle: string;
  now: () => Date;
}

const LOADING: BoardViewModel = { phase: "loading", error: "", compact: false, columns: [], canvas: { w: 0, h: 0 }, cards: [], edges: [] };

export class BoardPresenter extends Presenter<BoardViewModel> {
  readonly #deps: BoardDeps;
  #sessions: BoardSession[] = [];
  #compact = false;

  constructor(deps: BoardDeps) {
    super(LOADING);
    this.#deps = deps;
  }

  async start(): Promise<void> {
    try {
      this.#sessions = await this.#deps.source.sessions(this.#deps.ticketId);
    } catch (err) {
      if (!(err instanceof BoardSourceError)) throw err;
      this.set({ ...LOADING, phase: "error", error: err.message });
      return;
    }
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
    const rects: Record<string, Rect> = {};
    for (const [id, p] of Object.entries(layout.positions)) rects[id] = { ...p, ...sizeOf(id === YOU_ID, compact) };
    const cards: CardViewModel[] = [
      { id: YOU_ID, kind: "you", rect: rects[YOU_ID]!, agent: "You", mode: "", model: "", branch: "", badge: "", state: "", word: "", chip: "", line: null, dimmed: false, tone: "", when: "", attention: false, name: "You" },
      ...inStartOrder(sessions).map((s) => this.#card(s, rects[s.id]!)),
    ];
    const ended = new Set(sessions.filter(isTerminal).map((s) => s.id));
    const edges = startedEdges(sessions).map((e) => ({
      id: e.id,
      from: e.from,
      to: e.to,
      d: edgeGeometry(rects[e.from]!, rects[e.to]!).d,
      dimmed: ended.has(e.to),
    }));
    return { phase: "ready", error: "", compact, columns: layout.columns, canvas: layout.size, cards, edges };
  }

  #card(s: BoardSession, rect: Rect): CardViewModel {
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
      line: detailOf(s, { taskTitle, agentNames, others: this.#sessions, report: null, messagesLoaded: false }),
      dimmed: isTerminal(s) && s.status !== "failed",
      tone: s.status === "failed" ? "danger" : "",
      when: relativeTime(s.updatedAt, this.#deps.now()),
      attention: needsYou(s),
      name: [agentLabel(s.agentId, agentNames), s.role === "architect" ? "architect" : s.mode, status.word, isTerminal(s) && s.status !== "failed" ? "ended" : ""].filter(Boolean).join(", "),
    };
  }
}
