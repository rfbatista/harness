// What the writing helper needs from the outside world: one streamed step
// (POST /api/run_writing_helper_step). Implemented by
// infrastructure/step-gateway.js; testing/stub-gateway.js plays it for tests
// and the dev page.

/**
 * @typedef {"task_title" | "task_description" | "comment"} FieldKind
 *
 * @typedef {object} StepRequest
 * @property {string} project_id
 * @property {"analyze" | "draft" | "titles"} step
 * @property {{ kind: FieldKind, text: string }} field
 * @property {{ label: string, text: string }[]} [siblings]
 * @property {{ question: string, answer: string }[]} [answers]
 * @property {string} [draft]   the current proposal (a refine)
 * @property {string} [request] a chip's text or the person's own request (a refine)
 * @property {string[]} [exclude] titles already offered
 *
 * @typedef {{ id: string, text: string, why: string, chips: string[] }} Question
 * @typedef {{ already_good: boolean, questions: Question[] }} AnalyzeResult
 * @typedef {{ text: string }} DraftResult
 * @typedef {{ titles: string[] }} TitlesResult
 *
 * @typedef {object} StepGateway
 * @property {(request: StepRequest, options?: { signal?: AbortSignal, onDelta?: (text: string) => void }) => Promise<any>} runStep
 *           Runs one step. onDelta gets each piece of a draft as it is
 *           written; the promise resolves with the result event's data.
 *           Rejects with a StructuredError (WRITING_HELPER_UNAVAILABLE,
 *           WRITING_HELPER_FAILED, INVALID_INPUT, NETWORK) or an AbortError
 *           when the signal aborts.
 */

export {};
