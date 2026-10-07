// The architect channel as the markup binds it: task messages in the
// Conversation tab and their announcements.

import { relativeTime } from "../../../shared/presentation/format.js";
import { agentLabel } from "./view.js";

/** A body this long is folded to 8 lines until shown in full. */
const LONG_LINES = 8;
const LONG_CHARS = 800;

const KIND_WORD = { review_request: "review request", status_report: "status report", question: "question", reply: "reply" };

/**
 * A message's status or verdict as a badge. Teal only for work going on,
 * amber for what waits on someone, quiet for what is settled (DESIGN.md:
 * The Done Goes Quiet Rule). ready_for_review waits on the architect, not on
 * the person, so it is not amber.
 */
const BADGE = {
  working: { word: "working", tone: "signal" },
  blocked: { word: "blocked", tone: "attention" },
  ready_for_review: { word: "ready for review", tone: null },
  done: { word: "done", tone: null },
  approved: { word: "approved", tone: null },
  changes_requested: { word: "changes requested", tone: "attention" },
};

/**
 * @typedef {object} ChannelContext
 * @property {import("../domain/session.js").Session[]} sessions  the task's sessions
 * @property {Record<string, string>} agentNames
 * @property {Record<string, string>} documentTitles   the task's documents when the page loaded
 * @property {Record<string, { title: string, href: string }>} artifactTitles  artifacts looked up so far
 * @property {string} projectId
 * @property {string} ticketId
 * @property {Date} now
 * @property {import("../domain/channel.js").TaskMessage[]} [messages]  to name what a reply answers
 */

/** Who a message is from or to: "the architect", its agent, or a session that is gone. */
export function partyLabel(sessionId, { sessions, agentNames }) {
  const s = sessions.find((x) => x.id === sessionId);
  if (!s) return `a deleted session ${sessionId.slice(0, 8)}`;
  if (s.role === "architect") return "the architect";
  return agentLabel(s.agentId, agentNames);
}

/** The first line of a message, to name it from a reply. */
const headline = (m) => m.subject || m.body.split("\n")[0];

/**
 * @param {import("../domain/channel.js").TaskMessage} m
 * @param {ChannelContext} ctx
 */
export function messageView(m, ctx) {
  const p = encodeURIComponent(ctx.projectId);
  const t = encodeURIComponent(ctx.ticketId);
  const answered = m.inReplyTo ? (ctx.messages ?? []).find((x) => x.id === m.inReplyTo) : null;
  const documents = m.documentIds.map((id) => ({
    id,
    title: ctx.documentTitles[id] || "Document",
    href: `/projects/${p}/tasks/${t}/documents/${encodeURIComponent(id)}`,
  }));
  const artifacts = m.artifactIds.map((id) => ctx.artifactTitles[id] ? { id, ...ctx.artifactTitles[id] } : { id, title: `Artifact ${id.slice(0, 8)}`, href: "" });
  return {
    id: m.id,
    from: partyLabel(m.fromSessionId, ctx),
    to: partyLabel(m.toSessionId, ctx),
    kind: KIND_WORD[m.kind],
    badges: [BADGE[m.status], BADGE[m.verdict]].filter(Boolean),
    subject: m.subject,
    body: m.body,
    long: m.body.split("\n").length > LONG_LINES || m.body.length > LONG_CHARS,
    age: relativeTime(m.createdAt, ctx.now),
    replyTo: m.inReplyTo ? (answered ? headline(answered) : "an earlier message") : "",
    documents,
    artifacts,
    hasLinks: documents.length + artifacts.length > 0,
    undelivered: !m.delivered,
  };
}

/** What the polite live region says when a message arrives. */
export function announceMessage(m, ctx) {
  const from = partyLabel(m.fromSessionId, ctx);
  switch (m.kind) {
    case "status_report":
      return `${from} reported: ${BADGE[m.status]?.word ?? "an update"}`;
    case "question":
      return `${from} asked the architect a question`;
    case "review_request":
      return `${from} asked the architect for a review`;
    default: {
      const verdict = BADGE[m.verdict]?.word;
      return `${from} replied to ${partyLabel(m.toSessionId, ctx)}${verdict ? `: ${verdict}` : ""}`;
    }
  }
}
