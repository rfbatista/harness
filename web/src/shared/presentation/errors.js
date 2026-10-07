// How an error reads to a person: what happened, the code, and what to do.
// Rendered by the .banner block (message line + .code line).

import { Codes, codeOf } from "../domain/errors.js";

const NEXT_STEP = {
  [Codes.NETWORK]: "check that `make air` is running, then retry",
  [Codes.SESSION_NOT_FOUND]: "it was deleted; the list will refresh",
  [Codes.SESSION_NOT_RUNNING]: "the session has ended; start a new one to continue",
  [Codes.PROJECT_NOT_FOUND]: "pick another project at the top",
  [Codes.TICKET_NOT_FOUND]: "it was deleted; pick a task on the rail",
  [Codes.DOCUMENT_NOT_FOUND]: "it was deleted; reload the page",
  [Codes.INVALID_ROOT]: "give the absolute path of the project's directory on the server's machine",
  [Codes.INVALID_URL]: "give the repository's git remote, or leave it empty for a local-only one",
  [Codes.REPOSITORY_NOT_FOUND]: "it was already removed; reload the page",
  [Codes.INVALID_PATH]: "use a path inside the repository, like .env or apps/api/.env",
  [Codes.ENV_FILE_NOT_FOUND]: "the checkout has no such file; add it empty and paste its variables instead",
  [Codes.RUN_NOT_FOUND]: "it is gone (the server restarted?); start it again",
  [Codes.RUN_COMMAND_NOT_FOUND]: "it was renamed or removed; reload the page",
  [Codes.INVALID_NAME]: "use a short name: letters, digits, spaces, . _ -",
  [Codes.ENV_FILE_TOO_LARGE]: "keep env files under 256 KB: they hold variables, not data",
  [Codes.INVALID_INPUT]: "fix the form and try again",
  [Codes.INVALID_STATUS]: "pick one of the listed statuses",
  [Codes.CROSS_PROJECT_ACCESS]: "pick a repository of this project",
  [Codes.CLAUDE_CLI_NOT_FOUND]: "install the Claude CLI on the server's machine, then retry",
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
