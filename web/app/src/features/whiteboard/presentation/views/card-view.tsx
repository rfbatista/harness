// One card: the session row turned into a tile.

import { useRef, type CSSProperties } from "react";
import type { BoardPresenter, CardViewModel } from "../presenters/board-presenter";

/** Pixels the pointer must travel before a press is a drag and not a click. */
const DRAG_SLOP = 4;

export function CardView({ card, presenter }: { card: CardViewModel; presenter: BoardPresenter }) {
  const { rect } = card;
  const grab = useRef<{ x: number; y: number; moved: boolean } | null>(null);
  /** The press that just ended a drag also clicks: that click selects nothing. */
  const dragged = useRef(false);

  const nav = {
    tabIndex: 0,
    onFocus: () => presenter.focus(card.id),
    onClick: () => {
      if (dragged.current) dragged.current = false;
      else presenter.open(card.id);
    },
    onDoubleClick: () => card.kind === "session" && presenter.select(card.id, "context"),
  };
  const drag = {
    onPointerDown: (event: React.PointerEvent<HTMLButtonElement>) => {
      if (card.kind === "you" || event.button !== 0) return;
      grab.current = { x: event.clientX, y: event.clientY, moved: false };
      event.currentTarget.setPointerCapture(event.pointerId);
    },
    onPointerMove: (event: React.PointerEvent<HTMLButtonElement>) => {
      const g = grab.current;
      if (!g) return;
      const dx = event.clientX - g.x;
      const dy = event.clientY - g.y;
      if (!g.moved && Math.hypot(dx, dy) < DRAG_SLOP) return;
      if (!g.moved) presenter.beginDrag(card.id);
      g.moved = true;
      presenter.dragTo(card.id, dx, dy);
    },
    onPointerUp: () => {
      if (grab.current?.moved) {
        dragged.current = true;
        presenter.endDrag(card.id);
      }
      grab.current = null;
    },
    onPointerCancel: () => {
      if (grab.current?.moved) presenter.endDrag(card.id);
      grab.current = null;
    },
  };
  const style = { left: rect.x, top: rect.y, inlineSize: rect.w, minBlockSize: rect.h };
  if (card.kind === "you") {
    return (
      <button type="button" className="wb-node" role="listitem" data-id="you" data-role="person" data-attention={card.attention ? "" : undefined} aria-label={card.name} style={style} {...nav}>
        <span className="status" data-state={card.state} data-dot-only />
        <span className="who">
          <span className="agent">You</span>
        </span>
      </button>
    );
  }
  return (
    <button
      type="button"
      className="wb-node"
      role="listitem"
      data-id={card.id}
      data-role={card.badge === "architect" ? "architect" : undefined}
      data-ended={card.dimmed ? "" : undefined}
      data-attention={card.attention ? "" : undefined}
      data-selected={card.selected ? "" : undefined}
      aria-current={card.selected ? "true" : undefined}
      data-fresh={card.ringed ? "" : undefined}
      data-ended-process={card.memoryRinged ? "" : undefined}
      data-dragging={card.dragging ? "" : undefined}
      aria-label={card.name}
      title={card.memory?.sentence}
      style={style}
      {...nav}
      {...drag}
    >
      <span className="status" data-state={card.state} data-dot-only>
        <span className="visually-hidden">{card.word}</span>
      </span>
      <span className="who">
        <span className="agent">{card.agent}</span>
        {card.badge && <span className="badge">{card.badge}</span>}
        {card.mode && <span className="badge" data-tone="signal">{card.mode}</span>}
        {card.model && <span className="badge">{card.model}</span>}
        {card.chip && <span className="badge" data-tone={card.tone === "danger" ? "danger" : card.attention ? "attention" : undefined}>{card.chip}</span>}
        {card.memory?.badge && <span className="badge" data-mem data-tone={card.memory.state === "near" ? "attention" : card.memory.state === "normal" ? undefined : "danger"}>{card.memory.badge}</span>}
      </span>
      <time className="when">{card.when}</time>
      <span className="branch">
        <span className="name" title={card.branch}>
          <bdi>{card.branch}</bdi>
        </span>
        {card.pushState && <span className="badge">{card.pushState}</span>}
        {card.unapproved > 0 && <span className="badge" data-tone="attention">⚑ {card.unapproved} unapproved</span>}
      </span>
      {card.line && (
        <span className="detail">
          <span className="lead">{card.line.lead}</span> {card.line.text}
        </span>
      )}
      {card.memory && (
        <span className="mem" data-state={card.memory.state} style={{ "--_fill": `${card.memory.fillPercent}%` } as CSSProperties}>
          <span className="track" aria-hidden="true">
            <span className="fill" />
          </span>
          {card.memory.ended ? (
            <span className="word">
              <code>{card.memory.ended.command}</code> ended · {card.memory.ended.age}
            </span>
          ) : (
            <>
              <span className="num">
                {card.memory.num}
                <span className="of">{card.memory.of}</span>
              </span>
              {card.memory.word && <span className="word">{card.memory.word}</span>}
            </>
          )}
        </span>
      )}
    </button>
  );
}
