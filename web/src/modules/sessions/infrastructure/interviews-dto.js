// The interview contract's wire shapes (GET /api/sessions/{id}/interviews, the
// seed's `interviews`, the per-session interview feed) as the model reads them.

import { Codes, StructuredError } from "../../../shared/domain/errors.js";

const STATES = ["open", "sent", "dismissed", "expired"];
const KINDS = ["single", "multiple", "text"];

function bad(detail) {
  throw new StructuredError(Codes.BAD_RESPONSE, `Unexpected interview data: ${detail}.`);
}

/** An optional RFC 3339 time: null when absent, BAD_RESPONSE when unreadable. */
function optionalDate(value, what) {
  if (value === undefined || value === null || value === "") return null;
  const d = new Date(value);
  if (Number.isNaN(d.getTime())) bad(`${what} is not a time`);
  return d;
}

const text = (v) => (typeof v === "string" ? v : "");

function toRecommended(dto) {
  if (dto === null || dto === undefined) return null;
  if (Number.isInteger(dto.option)) return Object.freeze({ option: dto.option });
  if (typeof dto.text === "string") return Object.freeze({ text: dto.text });
  return null;
}

function toQuestion(dto) {
  if (!dto || typeof dto.title !== "string" || !KINDS.includes(dto.kind)) bad("a question without a title or a kind");
  const options = Array.isArray(dto.options) ? dto.options : [];
  return Object.freeze({
    title: dto.title,
    body: text(dto.body),
    kind: dto.kind,
    options: Object.freeze(options.map((o) => Object.freeze({ label: text(o?.label), description: text(o?.description) }))),
    recommended: toRecommended(dto.recommended),
  });
}

function toAnswer(dto) {
  return Object.freeze({
    chosen: Object.freeze(Array.isArray(dto?.chosen) ? dto.chosen.filter(Number.isInteger) : []),
    text: text(dto?.text),
    note: text(dto?.note),
    skipped: dto?.skipped === true,
  });
}

/** @returns {import("../domain/interviews.js").Interview} */
export function toInterview(dto) {
  if (!dto || typeof dto.id !== "string" || dto.id === "") bad("an interview without an id");
  if (!STATES.includes(dto.state)) bad(`interview ${dto.id} has the state ${JSON.stringify(dto.state)}`);
  if (!Array.isArray(dto.questions)) bad(`interview ${dto.id} has no questions`);
  return Object.freeze({
    id: dto.id,
    sessionId: text(dto.session_id),
    agentName: text(dto.agent_name) || "claude",
    title: text(dto.title),
    askedAt: optionalDate(dto.asked_at, "interview: asked_at") ?? new Date(0),
    state: dto.state,
    endedAt: optionalDate(dto.ended_at, "interview: ended_at"),
    delivery: dto.delivery === "delivered" || dto.delivery === "queued" ? dto.delivery : null,
    deliveredAt: optionalDate(dto.delivered_at, "interview: delivered_at"),
    questions: Object.freeze(dto.questions.map(toQuestion)),
    answers: Array.isArray(dto.answers) ? Object.freeze(dto.answers.map(toAnswer)) : null,
    dismissNote: typeof dto.dismiss_note === "string" ? dto.dismiss_note : null,
  });
}

/** {"seq": n, "interviews": [...]} */
export function toInterviewList(body) {
  if (!body || !Number.isInteger(body.seq) || !Array.isArray(body.interviews)) bad("expected {seq, interviews: [...]}");
  return Object.freeze({ seq: body.seq, interviews: Object.freeze(body.interviews.map(toInterview)) });
}

/** One `data:` line of a session's interview feed. */
export function toInterviewEvent(dto) {
  if (!dto || !Number.isInteger(dto.seq) || (dto.kind !== "interview_asked" && dto.kind !== "interview_changed")) bad("an event that is not interview_asked or interview_changed");
  return Object.freeze({ seq: dto.seq, kind: dto.kind, interview: toInterview(dto.interview) });
}

/**
 * The seed's `interviews`: each session's list by its id. A session whose list
 * cannot be read is left out, so its panel reads it from the API instead of
 * the page failing.
 */
export function toSeedInterviews(map) {
  const out = {};
  for (const [id, body] of Object.entries(map ?? {})) {
    try {
      out[id] = toInterviewList(body);
    } catch {
      // read again when its panel opens
    }
  }
  return out;
}
