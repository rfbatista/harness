// The answers a person has typed into an interview and not sent, kept in this
// browser per interview id for 7 days (ADR 0037: per-browser, never sent to the
// server). Storage can be missing or throw (private windows), so every access
// is guarded and answering works without it. An entry older than 7 days, or
// one that does not fit the interview, is removed when read; `sweep` removes
// every expired one.

import { isAnswerState } from "../domain/answers.js";

/** How long an unsent draft is kept, counted from its last change. */
export const DRAFT_DAYS = 7;
export const DRAFT_PREFIX = "harness:interview-draft:";

const DAY_MS = 86_400_000;

/**
 * @param {{ storage?: Storage | null, now?: () => number }} [deps]
 */
export function interviewDrafts({ storage = safeLocalStorage(), now = Date.now } = {}) {
  const guard = (fn, fallback) => {
    try {
      return storage ? fn(storage) : fallback;
    } catch {
      return fallback; // not kept; the answers still apply to this page
    }
  };
  const expired = (at) => !(now() - at <= DRAFT_DAYS * DAY_MS);
  const parse = (raw) => {
    try {
      const entry = JSON.parse(raw);
      return typeof entry?.at === "number" && Array.isArray(entry.answers) ? entry : null;
    } catch {
      return null;
    }
  };

  return {
    /**
     * @param {string} interviewId
     * @param {number} questions  how many the interview has now: a draft for another shape is no draft
     * @returns {import("../domain/answers.js").AnswerState[] | null}
     */
    read(interviewId, questions) {
      return guard((s) => {
        const raw = s.getItem(DRAFT_PREFIX + interviewId);
        if (raw === null) return null;
        const entry = parse(raw);
        if (!entry || expired(entry.at) || entry.answers.length !== questions || !entry.answers.every(isAnswerState)) {
          s.removeItem(DRAFT_PREFIX + interviewId);
          return null;
        }
        return entry.answers;
      }, null);
    },

    /** Keeps the answers as they are now, and the 7 days start again. */
    write(interviewId, answers) {
      guard((s) => s.setItem(DRAFT_PREFIX + interviewId, JSON.stringify({ at: now(), answers })));
    },

    /** The interview is sent, dismissed or gone: nothing is left to keep. */
    drop(interviewId) {
      guard((s) => s.removeItem(DRAFT_PREFIX + interviewId));
    },

    /** Removes every draft older than 7 days. */
    sweep() {
      guard((s) => {
        const keys = Array.from({ length: s.length }, (_, n) => s.key(n)).filter((k) => k?.startsWith(DRAFT_PREFIX));
        for (const key of keys) {
          const entry = parse(s.getItem(key) ?? "");
          if (!entry || expired(entry.at)) s.removeItem(key);
        }
      });
    },
  };
}

function safeLocalStorage() {
  try {
    return globalThis.localStorage ?? null;
  } catch {
    return null;
  }
}
