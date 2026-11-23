// One card: the session row turned into a tile.

import type { CardViewModel } from "../presenters/board-presenter";

export function CardView({ card }: { card: CardViewModel }) {
  const { rect } = card;
  const style = { left: rect.x, top: rect.y, inlineSize: rect.w, blockSize: rect.h };
  if (card.kind === "you") {
    return (
      <button type="button" className="wb-node" role="listitem" data-id="you" data-role="person" aria-label={card.name} style={style}>
        <span className="status" data-state="idle" data-dot-only />
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
      aria-label={card.name}
      style={style}
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
      </span>
      <time className="when">{card.when}</time>
      <span className="branch">
        <span className="name" title={card.branch}>
          <bdi>{card.branch}</bdi>
        </span>
      </span>
      {card.line && (
        <span className="detail">
          <span className="lead">{card.line.lead}</span> {card.line.text}
        </span>
      )}
    </button>
  );
}
