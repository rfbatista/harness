// The browser side of the server's coded error contract
// (httpapi/errors.go: {"error": msg, "code": CODE}). Code branches on
// `code`, never on `message`.

/** Codes the web client reacts to. The server owns the full list. */
export const Codes = Object.freeze({
  SESSION_NOT_FOUND: "SESSION_NOT_FOUND",
  SESSION_NOT_RUNNING: "SESSION_NOT_RUNNING",
  PROJECT_NOT_FOUND: "PROJECT_NOT_FOUND",
  TICKET_NOT_FOUND: "TICKET_NOT_FOUND",
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
   */
  constructor(code, message, status) {
    super(message);
    this.name = "StructuredError";
    this.code = code;
    this.status = status;
  }
}

/** The code of any thrown value: a StructuredError's own, otherwise UNKNOWN. */
export function codeOf(err) {
  return err instanceof StructuredError ? err.code : Codes.UNKNOWN;
}
