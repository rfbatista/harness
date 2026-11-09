// What the person has answered in the open interviews, for the store
// "sessionsInterviews" (spread into it): the answers per interview id, the
// changes the panel's controls make, the counts the footer shows, and the
// draft each change leaves in this browser. Nothing here sends: tickets that
// send or dismiss read `submission(id)` and call `drop(id)`. An expired
// interview keeps what was typed, to be read back (`typedAnswers`).

import { acceptOne, acceptRecommended, blankAnswers, isAnswered, progress, selectOption, selectOwn, toAnswers, withNote, withText } from "../domain/answers.js";
import { openOnes } from "../domain/interviews.js";

/**
 * @param {{ read: Function, write: Function, drop: Function }} drafts  the browser's unsent answers (infrastructure/interviewDrafts.js)
 */
export function answering(drafts) {
  return {
    /** @type {Record<string, import("../domain/answers.js").AnswerState[]>} by interview id: one state per question */
    answers: {},

    /** Takes the answers of every open interview of `lists` that holds none yet (from its draft, else blank), and lets go of the drafts of those no longer open. */
    hydrate(lists) {
      const next = { ...this.answers };
      for (const list of Object.values(lists)) {
        for (const interview of list.interviews) {
          if (interview.state === "expired") {
            // What was typed stays, to be read back and copied: it is all that is left of it.
            const kept = interview.id in next ? null : drafts.read(interview.id, interview.questions.length);
            if (kept) next[interview.id] = kept;
          } else if (interview.state !== "open") {
            delete next[interview.id];
            drafts.drop(interview.id);
          } else if (!(interview.id in next)) {
            next[interview.id] = drafts.read(interview.id, interview.questions.length) ?? blankAnswers(interview.questions);
          }
        }
      }
      this.answers = next;
    },

    /** Whether the person has put anything into the open interview: an option, text or a note. */
    touched(id) {
      return (this.answers[id] ?? []).some((s) => s.chosen.length > 0 || s.own || s.text.trim() !== "" || s.note.trim() !== "");
    },

    /** The open interview with this id, or null. */
    openInterview(id) {
      for (const list of Object.values(this.lists)) {
        const found = openOnes(list).find((i) => i.id === id);
        if (found) return found;
      }
      return null;
    },

    /** The interview's answers, one state per question: blank for one that holds none, empty for one that is not open. */
    answersFor(id) {
      return this.answers[id] ?? blankAnswers(this.openInterview(id)?.questions ?? []);
    },

    /** How many questions are answered and how many a send would skip. */
    progress(id) {
      const interview = this.openInterview(id);
      return interview ? progress(interview.questions, this.answersFor(id)) : { answered: 0, total: 0, skipped: 0 };
    },

    /** The contract's answers [{chosen, text, note, skipped}] for what is typed now; null when the interview is not open. */
    submission(id) {
      const interview = this.openInterview(id);
      return interview ? toAnswers(interview.questions, this.answersFor(id)) : null;
    },

    /** Whether question `at` holds an answer. */
    answered(id, at) {
      const q = this.openInterview(id)?.questions[at];
      return !!q && isAnswered(q, this.answersFor(id)[at]);
    },

    /** Whether the option whose value is `value` ("own" for Write my own) is selected. */
    isChecked(id, at, value) {
      const s = this.answersFor(id)[at];
      if (!s) return false;
      return value === "own" ? s.own : s.chosen.includes(Number(value));
    },

    /** Whether Write my own is selected: its text box is drawn. */
    ownOpen(id, at) {
      return this.answersFor(id)[at]?.own === true;
    },

    textOf(id, at) {
      return this.answersFor(id)[at]?.text ?? "";
    },

    noteOf(id, at) {
      return this.answersFor(id)[at]?.note ?? "";
    },

    /** An option (or Write my own, value "own") ticked or unticked; a single choice only ever selects. */
    choose(id, at, value, on = true) {
      this.change(id, at, (q, s) => (value === "own" ? selectOwn(q, s, on) : selectOption(q, s, Number(value), on)));
    },

    /** The text of a text question, or of Write my own. */
    type(id, at, text) {
      this.change(id, at, (q, s) => withText(s, text));
    },

    note(id, at, text) {
      this.change(id, at, (q, s) => withNote(s, text));
    },

    /** Takes the agent's recommendation for one question (a text one: "Use this text"). */
    use(id, at) {
      this.change(id, at, acceptOne);
    },

    /** Accept all recommended: the blank questions that have a recommendation, never an answer already given. Sends nothing. */
    acceptAll(id) {
      const interview = this.openInterview(id);
      if (interview) this.put(id, acceptRecommended(interview.questions, this.answersFor(id)));
    },

    /** The interview is sent or dismissed (or gone): its answers and its draft are forgotten. */
    drop(id) {
      const { [id]: _, ...rest } = this.answers;
      this.answers = rest;
      drafts.drop(id);
    },

    change(id, at, fn) {
      const interview = this.openInterview(id);
      const states = this.answersFor(id);
      if (!interview || !states[at]) return;
      this.put(id, states.map((s, n) => (n === at ? fn(interview.questions[at], s) : s)));
    },

    put(id, states) {
      this.answers = { ...this.answers, [id]: states };
      drafts.write(id, states);
    },
  };
}
