// The Agent board's state and the rules that change it: what a seed makes of
// it, what each live session feed event does, and how a seed read again is
// reconciled with what is on screen (ADR 0068; "Contract: harness server ↔
// web UI — the agent board seed and the live session feed").
//
// needs_you comes from the server on every card and is only read here, never
// derived from the sessions. Order rule: a card's place changes only when it
// arrives, starts needing the person, is removed, and when a seed is read.
//
// State is plain and replaced, never mutated:
//   { seq, projectCount, quietProjects, sections: [{ id, name, cards: [Card] }] }
// A card on its way out has leaving: true (its last session ended; the page
// holds it for a moment before it goes).

/**
 * @typedef {object} SessionLine
 * @property {string} sessionId
 * @property {string} agentName
 * @property {string} role
 * @property {string} status  starting, running, thinking, idle, waiting_approval, paused, or finished (ended on the board)
 * @property {Date} statusSince
 * @property {string|null} waitingFor
 * @property {string} mode
 * @property {string} model
 *
 * @typedef {object} Card
 * @property {string} taskId
 * @property {string} projectId
 * @property {string} taskTitle
 * @property {string} taskStatus
 * @property {string|null} featureId
 * @property {string|null} featureTitle
 * @property {boolean} needsYou
 * @property {Date|null} needsYouSince
 * @property {Date} lastStatusChangeAt
 * @property {SessionLine[]} sessions
 * @property {boolean} [leaving]
 */

export const FINISHED = "finished";

export const emptyBoard = () => ({ seq: 0, projectCount: 0, quietProjects: 0, sections: [] });

const byName = (a, b) => a.name.toLowerCase().localeCompare(b.name.toLowerCase()) || a.id.localeCompare(b.id);

/** The board a seed describes, cards in the seed's order. */
export function fromSeed(seed) {
  return {
    seq: seed.seq,
    projectCount: seed.projectCount,
    quietProjects: seed.quietProjects,
    sections: seed.projects.map((p) => ({ id: p.projectId, name: p.projectName, cards: p.cards.map((c) => ({ ...c })) })),
  };
}

/** Cards that are not on their way out. */
export const liveCards = (section) => section.cards.filter((c) => !c.leaving);

/** Where a card is: { s, c } indexes, or null. */
function locate(board, taskId) {
  for (let s = 0; s < board.sections.length; s++) {
    const c = board.sections[s].cards.findIndex((card) => card.taskId === taskId);
    if (c >= 0) return { s, c };
  }
  return null;
}

export const findCard = (board, taskId) => {
  const at = locate(board, taskId);
  return at ? board.sections[at.s].cards[at.c] : null;
};

export const projectName = (board, projectId) => board.sections.find((s) => s.id === projectId)?.name ?? "";

const withSection = (board, index, section) => ({ ...board, sections: board.sections.map((s, i) => (i === index ? section : s)) });

/** Removes a card and, with it, a section left empty. */
export function removeCard(board, taskId) {
  const at = locate(board, taskId);
  if (!at) return board;
  const section = board.sections[at.s];
  const cards = section.cards.filter((c) => c.taskId !== taskId);
  if (cards.length === 0) return { ...board, sections: board.sections.filter((_, i) => i !== at.s) };
  return withSection(board, at.s, { ...section, cards });
}

/** A card whose last session ended: its lines read "finished" until it goes. */
function finished(card) {
  return {
    ...card,
    needsYou: false,
    needsYouSince: null,
    leaving: true,
    sessions: card.sessions.map((s) => ({ ...s, status: FINISHED, waitingFor: null })),
  };
}

/**
 * Places a card the server sent whole. A known card is replaced where it is,
 * and moves only when it starts needing the person (to the front of its
 * section). A new card goes to the head of the cards that do not need the
 * person, or to the front when it does; a new project makes its section in
 * name order.
 * @returns {{ board: object, notes: object[], needsSeed?: boolean }}
 */
export function placeCard(board, card, name = "") {
  const at = locate(board, card.taskId);
  const notes = [];
  if (at) {
    const section = board.sections[at.s];
    const old = section.cards[at.c];
    const next = { ...card, leaving: false };
    if (old.leaving) notes.push({ type: "revived", taskId: card.taskId });
    for (const line of next.sessions) {
      const before = old.sessions.find((l) => l.sessionId === line.sessionId);
      if (!before || before.status !== line.status) notes.push({ type: "line", taskId: card.taskId, sessionId: line.sessionId });
    }
    const cards = section.cards.map((c, i) => (i === at.c ? next : c));
    if (next.needsYou && !old.needsYou) {
      cards.splice(at.c, 1);
      cards.unshift(next);
      notes.push({ type: "needs", taskId: card.taskId, projectId: card.projectId });
    }
    return { board: withSection(board, at.s, { ...section, cards }), notes };
  }
  let sIndex = board.sections.findIndex((s) => s.id === card.projectId);
  let sections = board.sections;
  if (sIndex < 0) {
    if (!name) return { board, notes, needsSeed: true };
    sections = [...sections, { id: card.projectId, name, cards: [] }].sort(byName);
    sIndex = sections.findIndex((s) => s.id === card.projectId);
  }
  const section = sections[sIndex];
  const head = card.needsYou ? 0 : section.cards.findIndex((c) => !c.needsYou);
  const cards = [...section.cards];
  cards.splice(head < 0 ? cards.length : head, 0, { ...card, leaving: false });
  notes.push({ type: card.needsYou ? "needs" : "arrived", taskId: card.taskId, projectId: card.projectId });
  return { board: { ...board, sections: sections.map((s, i) => (i === sIndex ? { ...section, cards } : s)) }, notes };
}

/**
 * What one feed event does to the board.
 * outcome: "stale" (seq at or below the last applied: discarded), "gap" (seq
 * more than one past it: read the seed again), "reseed" (the event cannot be
 * drawn without the seed), or "applied".
 * @returns {{ outcome: string, board: object, notes: object[] }}
 */
export function applyEvent(board, event) {
  if (event.seq <= board.seq) return { outcome: "stale", board, notes: [] };
  if (event.seq > board.seq + 1) return { outcome: "gap", board, notes: [] };
  const next = { ...board, seq: event.seq };
  const done = (b, notes = []) => ({ outcome: "applied", board: b, notes });
  switch (event.kind) {
    case "session_live":
    case "task_changed": {
      if (!event.card || event.card.sessions.length === 0) return done(removeCard(next, event.taskId));
      const placed = placeCard(next, event.card, event.projectName);
      return placed.needsSeed ? { outcome: "reseed", board, notes: [] } : done(placed.board, placed.notes);
    }
    case "session_ended": {
      const known = findCard(next, event.taskId);
      if (!event.card || event.card.sessions.length === 0) {
        if (!known) return done(next);
        const ended = finished(known);
        return done(replaceCard(next, ended), [{ type: "leaving", taskId: event.taskId }]);
      }
      const placed = placeCard(next, event.card, event.projectName);
      return placed.needsSeed ? { outcome: "reseed", board, notes: [] } : done(placed.board, placed.notes);
    }
    case "project_changed": {
      const i = next.sections.findIndex((s) => s.id === event.projectId);
      if (i < 0) return done(next);
      const sections = next.sections.map((s, j) => (j === i ? { ...s, name: event.projectName } : s)).sort(byName);
      return done({ ...next, sections });
    }
    default:
      return done(next);
  }
}

function replaceCard(board, card) {
  const at = locate(board, card.taskId);
  const section = board.sections[at.s];
  return withSection(board, at.s, { ...section, cards: section.cards.map((c, i) => (i === at.c ? card : c)) });
}

/**
 * The board after a seed is read again. The seed's order wins (a seed is
 * where the order is read again). A card the seed no longer has leaves the
 * way an ended one does, so nothing vanishes under the pointer; a card that
 * was already leaving stays until its hold ends. wholesale: true takes the
 * seed as it is (its seq is lower than the last applied: the server restarted).
 * @returns {{ board: object, notes: object[] }}
 */
export function reconcile(board, seed, { wholesale = false } = {}) {
  const next = fromSeed(seed);
  if (wholesale) return { board: next, notes: [] };
  const notes = [];
  for (const section of next.sections) {
    for (const card of section.cards) {
      const old = findCard(board, card.taskId);
      if (!old) notes.push({ type: "arrived", taskId: card.taskId, projectId: card.projectId });
      else {
        for (const line of card.sessions) {
          const before = old.sessions.find((l) => l.sessionId === line.sessionId);
          if (!before || before.status !== line.status) notes.push({ type: "line", taskId: card.taskId, sessionId: line.sessionId });
        }
        if (old.leaving) notes.push({ type: "revived", taskId: card.taskId });
      }
    }
  }
  const sections = [...next.sections];
  for (const section of board.sections) {
    for (const old of section.cards) {
      if (findCard({ sections }, old.taskId)) continue;
      const gone = old.leaving ? old : finished(old);
      if (!old.leaving) notes.push({ type: "leaving", taskId: old.taskId });
      const i = sections.findIndex((s) => s.id === section.id);
      if (i >= 0) sections[i] = { ...sections[i], cards: [...sections[i].cards, gone] };
      else sections.push({ id: section.id, name: section.name, cards: [gone] });
    }
  }
  sections.sort(byName);
  return { board: { ...next, sections }, notes };
}
