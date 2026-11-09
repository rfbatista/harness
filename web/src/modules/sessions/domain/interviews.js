// A session's interviews: the questions its agent asks the person and what
// became of them. Mirrors internal/domain/interview.go and "Contract:
// harness server ↔ web UI — interviews"; no I/O, no Alpine, no DOM.

/**
 * @typedef {"single"|"multiple"|"text"} QuestionKind
 * @typedef {{ label: string, description: string }} InterviewOption
 * @typedef {{ option: number } | { text: string } | null} Recommended
 * @typedef {{ title: string, body: string, kind: QuestionKind, options: readonly InterviewOption[], recommended: Recommended }} Question
 * @typedef {{ chosen: readonly number[], text: string, note: string, skipped: boolean }} Answer
 *
 * @typedef {object} Interview
 * @property {string} id
 * @property {string} sessionId
 * @property {string} agentName
 * @property {string} title  "" when the agent gave none
 * @property {Date} askedAt
 * @property {"open"|"sent"|"dismissed"|"expired"} state
 * @property {Date | null} endedAt
 * @property {"delivered"|"queued"|null} delivery
 * @property {Date | null} deliveredAt
 * @property {readonly Question[]} questions
 * @property {readonly Answer[] | null} answers  only a sent interview has them
 * @property {string | null} dismissNote
 *
 * @typedef {{ seq: number, interviews: readonly Interview[] }} InterviewList
 *          Every change up to seq is in it; the feed's next event is seq + 1.
 */

/** A session's interview list as the seed carries it before anything is asked. */
export const emptyList = Object.freeze({ seq: 0, interviews: Object.freeze([]) });

/** The panel docks beside the terminal when the session column is at least this wide, in rem. */
export const DOCK_MIN_REM = 56;

/** Whether a session column of `widthPx`, at `remPx` to the rem, is wide enough to dock the panel. */
export const dockedAt = (widthPx, remPx) => widthPx >= DOCK_MIN_REM * remPx;

/** The open interviews. */
export const openOnes = (list) => list.interviews.filter((i) => i.state === "open");

/** How many questions the open interviews hold: what the toggle counts. */
export const openQuestionCount = (list) => openOnes(list).reduce((n, i) => n + i.questions.length, 0);

/**
 * What one feed event does to a list. `applied` carries the new list;
 * `stale` (already in the list) changes nothing; `gap` says an event was
 * missed, so the list must be read again.
 * @param {InterviewList} list
 * @param {{ seq: number, kind: string, interview: Interview }} event
 * @returns {{ outcome: "applied", list: InterviewList } | { outcome: "stale" | "gap" }}
 */
export function applyEvent(list, event) {
  if (event.seq <= list.seq) return { outcome: "stale" };
  if (event.seq > list.seq + 1) return { outcome: "gap" };
  const at = list.interviews.findIndex((i) => i.id === event.interview.id);
  const interviews = at < 0 ? [...list.interviews, event.interview] : list.interviews.map((i, n) => (n === at ? event.interview : i));
  return { outcome: "applied", list: Object.freeze({ seq: event.seq, interviews: Object.freeze(interviews) }) };
}

/** The ids of `next`'s open interviews that `before` did not have open: what just arrived. */
export function arrived(before, next) {
  const had = new Set(openOnes(before).map((i) => i.id));
  return openOnes(next).filter((i) => !had.has(i.id));
}

/** When an interview ended, or when it was asked while nothing says it ended: what the history sorts by. */
const endedOrAsked = (i) => (i.endedAt ?? i.askedAt).getTime();

/** The interviews that are not open (sent, dismissed, expired), the most recently ended first: the panel's Earlier. */
export const historyOf = (list) =>
  list.interviews
    .filter((i) => i.state !== "open")
    .sort((a, b) => (b.endedAt !== null) - (a.endedAt !== null) || endedOrAsked(b) - endedOrAsked(a) || a.id.localeCompare(b.id));

/** The interviews of `next` that `before` had open and are not open any more: what was answered, dismissed or expired meanwhile. */
export function closedNow(before, next) {
  const had = new Set(openOnes(before).map((i) => i.id));
  return next.interviews.filter((i) => i.state !== "open" && had.has(i.id));
}
