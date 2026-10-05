// How an error reads to a person: what happened, the code, and what to do.
// Rendered by the .banner block (message line + .code line).

import { Codes, codeOf } from "../domain/errors.js";

const NEXT_STEP = {
  [Codes.NETWORK]: "check that `make air` is running, then retry",
  [Codes.SESSION_NOT_FOUND]: "it was deleted; the list will refresh",
  [Codes.SESSION_NOT_RUNNING]: "the session has ended; start a new one to continue",
  [Codes.PROJECT_NOT_FOUND]: "open another project from Projects",
  [Codes.TICKET_NOT_FOUND]: "it was deleted; reopen it from Tickets",
  [Codes.BAD_RESPONSE]: "the server and the web client disagree; reload the page",
};

/**
 * @param {unknown} err
 * @returns {{ message: string, code: string, next: string }}
 */
export function describeError(err) {
  const code = codeOf(err);
  const message = err instanceof Error && err.message ? err.message : "Something went wrong.";
  return { message, code, next: NEXT_STEP[code] ?? "retry, or check the server log" };
}
