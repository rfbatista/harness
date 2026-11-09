// InterviewGateway over the harness HTTP API: GET /api/sessions/{id}/interviews
// and the session's interview feed (/sessions/{id}/interviews/events) on the page stream,
// and the answers: POST /api/interviews/{id}/answers and /api/interviews/{id}/dismiss.

import { StructuredError } from "../../../shared/domain/errors.js";
import { toInterview, toInterviewEvent, toInterviewList } from "./interviews-dto.js";

/**
 * @param {import("../../../shared/infrastructure/api.js").ApiClient} api
 * @param {import("../../../shared/infrastructure/feed.js").Feed} feed
 * @returns {import("../domain/ports.js").InterviewGateway}
 */
export function interviewsGateway(api, feed) {
  const path = (sessionId) => `/sessions/${encodeURIComponent(sessionId)}/interviews`;
  const answerPath = (id, verb) => `/interviews/${encodeURIComponent(id)}/${verb}`;

  /** Posts, and reads the interview of the 200; a refusal that carries the interview (expired, not open) has it read into the model. */
  async function answer(id, verb, body) {
    try {
      return toInterview((await api.post(answerPath(id, verb), body)).interview);
    } catch (err) {
      if (err instanceof StructuredError && err.details?.interview) {
        let interview = null;
        try {
          interview = toInterview(err.details.interview);
        } catch {
          // an interview we cannot read is no use to the panel; the refusal stands without it
        }
        throw new StructuredError(err.code, err.message, err.status, interview ? { ...err.details, interview } : undefined);
      }
      throw err;
    }
  }

  return {
    submit: (id, answers) => answer(id, "answers", { answers }),
    dismiss: (id, note) => answer(id, "dismiss", { note }),

    async list(sessionId, signal) {
      return toInterviewList(await api.get(path(sessionId), {}, signal));
    },

    follow(sessionId, onEvent, onStatus) {
      return feed.follow(`${path(sessionId)}/events`, {
        onMessage: (dto) => {
          let event;
          try {
            event = toInterviewEvent(dto);
          } catch {
            return; // one we cannot read is dropped; the next event shows the gap and the list is read again
          }
          onEvent(event);
        },
        onStatus,
      });
    },
  };
}
