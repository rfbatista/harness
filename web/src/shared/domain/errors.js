// The browser side of the server's coded error contract
// (httpapi/errors.go: {"error": msg, "code": CODE, "details"?}). Code branches on
// `code`, never on `message`.

/** Codes the web client reacts to. The server owns the full list. */
export const Codes = Object.freeze({
  SESSION_NOT_FOUND: "SESSION_NOT_FOUND",
  SESSION_NOT_RUNNING: "SESSION_NOT_RUNNING",
  SESSION_NOT_INTERACTIVE: "SESSION_NOT_INTERACTIVE",
  SESSION_ALREADY_RUNNING: "SESSION_ALREADY_RUNNING",
  WORKSPACE_MISSING: "WORKSPACE_MISSING",
  SESSION_TRANSCRIPT_MISSING: "SESSION_TRANSCRIPT_MISSING",
  PROJECT_NOT_FOUND: "PROJECT_NOT_FOUND",
  PROJECT_NAME_TAKEN: "PROJECT_NAME_TAKEN",
  PROJECT_ROOT_INVALID: "PROJECT_ROOT_INVALID",
  PROJECT_HAS_RUNNING_SESSIONS: "PROJECT_HAS_RUNNING_SESSIONS",
  TICKET_NOT_FOUND: "TICKET_NOT_FOUND",
  DOCUMENT_NOT_FOUND: "DOCUMENT_NOT_FOUND",
  ARTIFACT_NOT_FOUND: "ARTIFACT_NOT_FOUND",
  ARTIFACT_NOT_PROMOTABLE: "ARTIFACT_NOT_PROMOTABLE",
  ARTIFACT_NOT_IN_PROJECT: "ARTIFACT_NOT_IN_PROJECT",
  ARTIFACT_PROJECT_MISMATCH: "ARTIFACT_PROJECT_MISMATCH",
  ARTIFACT_PRODUCER_TASK: "ARTIFACT_PRODUCER_TASK",
  INVALID_INPUT: "INVALID_INPUT",
  INVALID_STATUS: "INVALID_STATUS",
  CROSS_PROJECT_ACCESS: "CROSS_PROJECT_ACCESS",
  CLAUDE_CLI_NOT_FOUND: "CLAUDE_CLI_NOT_FOUND",
  INVALID_ROOT: "INVALID_ROOT",
  INVALID_URL: "INVALID_URL",
  REPOSITORY_NOT_FOUND: "REPOSITORY_NOT_FOUND",
  INVALID_PATH: "INVALID_PATH",
  ENV_FILE_NOT_FOUND: "ENV_FILE_NOT_FOUND",
  ENV_FILE_TOO_LARGE: "ENV_FILE_TOO_LARGE",
  RUN_NOT_FOUND: "RUN_NOT_FOUND",
  RUN_COMMAND_NOT_FOUND: "RUN_COMMAND_NOT_FOUND",
  INVALID_NAME: "INVALID_NAME",
  STATUS_CHECK_NOT_FOUND: "STATUS_CHECK_NOT_FOUND",
  REVIEW_NOT_FOUND: "REVIEW_NOT_FOUND",
  REVIEW_NOT_PENDING: "REVIEW_NOT_PENDING",
  // Client-side codes: never sent by the server.
  NETWORK: "NETWORK",
  BAD_RESPONSE: "BAD_RESPONSE",
  UNKNOWN: "UNKNOWN",
});

export class StructuredError extends Error {
  /**
   * @param {string} code    stable machine-readable code
   * @param {string} message human-readable, shown as is
   * @param {number} [status] HTTP status, when it came from the API
   * @param {object} [details] what the error names, for the few codes that carry more
   *                           (PROJECT_HAS_RUNNING_SESSIONS: {sessions: [...]})
   */
  constructor(code, message, status, details) {
    super(message);
    this.name = "StructuredError";
    this.code = code;
    this.status = status;
    this.details = details;
  }
}

/** The code of any thrown value: a StructuredError's own, otherwise UNKNOWN. */
export function codeOf(err) {
  return err instanceof StructuredError ? err.code : Codes.UNKNOWN;
}
