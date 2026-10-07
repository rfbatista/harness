// How an error reads to a person: what happened, the code, and what to do.
// Rendered by the .banner block (message line + .code line).

import { Codes, codeOf } from "../domain/errors.js";

const NEXT_STEP = {
  [Codes.NETWORK]: "check that `make air` is running, then retry",
  [Codes.SESSION_NOT_FOUND]: "it was deleted; the list will refresh",
  [Codes.SESSION_NOT_RUNNING]: "the session has ended; start a new one to continue",
  [Codes.SESSION_NOT_INTERACTIVE]: "it runs without a terminal; there is nothing to resume",
  [Codes.SESSION_ALREADY_RUNNING]: "it is running again (another tab or the TUI resumed it); the list will refresh",
  [Codes.WORKSPACE_MISSING]: "its worktree was removed; start a new session",
  [Codes.SESSION_TRANSCRIPT_MISSING]: "claude kept no conversation for it; start a new session",
  [Codes.PROJECT_NOT_FOUND]: "pick another project at the top",
  [Codes.PROJECT_NAME_TAKEN]: "pick a name no other project uses",
  [Codes.PROJECT_ROOT_INVALID]: "give the absolute path of an existing directory on the server's machine (~ is fine)",
  [Codes.PROJECT_HAS_RUNNING_SESSIONS]: "stop its running sessions from their tasks, then delete again",
  [Codes.TICKET_NOT_FOUND]: "it was deleted; pick a task on the rail",
  [Codes.DOCUMENT_NOT_FOUND]: "it was deleted; reload the page",
  [Codes.ARTIFACT_NOT_FOUND]: "it was deleted; reload the page",
  [Codes.ARTIFACT_NOT_PROMOTABLE]: "only a file a live session published can be kept; ask the agent to publish it as a file, then move it",
  [Codes.ARTIFACT_NOT_IN_PROJECT]: "only a project asset can be attached; move it to the project first",
  [Codes.ARTIFACT_PROJECT_MISMATCH]: "that task belongs to another project; pick one of this project's tasks",
  [Codes.ARTIFACT_PRODUCER_TASK]: "this task made the asset, so it cannot be detached; move it back to the task or delete it instead",
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
  [Codes.STATUS_CHECK_NOT_FOUND]: "its loop is gone (the session ended?); the list will refresh",
  [Codes.REVIEW_NOT_FOUND]: "the architect's request is gone; reload the page",
  [Codes.REVIEW_NOT_PENDING]: "it was already answered or withdrawn; the list now shows where it stands",
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
