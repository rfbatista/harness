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
  [Codes.TASK_NOT_DONE]: "only a done task can be archived; move it to Done first",
  [Codes.TASK_ARCHIVED]: "it is archived; restore it from Archived first",
  [Codes.TICKET_PARENT_INVALID]: "a parent is a top-level task of the same project",
  [Codes.TICKET_HAS_CHILDREN]: "delete or move its child tasks first",
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
  [Codes.GIT_CREDENTIAL_NOT_FOUND]: "it was deleted; the list will refresh",
  [Codes.GIT_CREDENTIAL_NAME_TAKEN]: "pick a name no other credential uses",
  [Codes.GIT_CREDENTIAL_INVALID_KEY]: "give an OpenSSH or PEM private key, with its passphrase if it has one",
  [Codes.GIT_CREDENTIAL_HOST_MISMATCH]: "pick a credential for the origin's host",
  [Codes.GIT_CREDENTIAL_KIND_MISMATCH]: "an https origin takes a token and an ssh origin takes a key; pick one of that kind",
  [Codes.GIT_CREDENTIAL_MISSING]: "add a credential for that host in Git credentials",
  [Codes.GIT_CREDENTIAL_REJECTED]: "replace its secret in Git credentials, then retry",
  [Codes.GIT_CREDENTIAL_STORE_LOCKED]: "give the server its key (HARNESS_SECRET_KEY or secret.key in its data directory), then retry",
  [Codes.AGENT_CREDENTIAL_MISSING]: "add the credential in Agent isolation, then start again",
  [Codes.MANAGED_CLAUDE_MISSING]: "install it in Agent isolation, then start again",
  [Codes.MANAGED_CLAUDE_INSTALLING]: "it installs on its own; start again when it is done",
  [Codes.MANAGED_CLAUDE_INSTALL_FAILED]: "retry the install in Agent isolation, then start again",
  [Codes.SANDBOX_DRIVER_MISSING]: "install a driver or turn the sandbox off in Agent isolation, then start again",
  [Codes.BAD_RESPONSE]: "the server and the web client disagree; reload the page",
};

// The Agent isolation page's sections an agent refusal sends the person to.
const FIX_LINKS = {
  [Codes.AGENT_CREDENTIAL_MISSING]: { href: "/agent-isolation#credential", text: "Add one in Agent isolation settings" },
  [Codes.MANAGED_CLAUDE_MISSING]: { href: "/agent-isolation#claude", text: "Install it in Agent isolation settings" },
  [Codes.MANAGED_CLAUDE_INSTALLING]: { href: "/agent-isolation#claude", text: "Follow the install in Agent isolation settings" },
  [Codes.MANAGED_CLAUDE_INSTALL_FAILED]: { href: "/agent-isolation#claude", text: "Retry the install in Agent isolation settings" },
  [Codes.SANDBOX_DRIVER_MISSING]: { href: "/agent-isolation#sandbox", text: "Install a driver or turn the sandbox off" },
};

/**
 * One link per thing a refused agent start is missing: the error's details
 * list them all, so the person fixes them in one visit.
 * @param {unknown} err
 * @returns {{ href: string, text: string }[]}
 */
function fixLinks(err) {
  const missing = /** @type {{ missing?: { code: string }[] }} */ (err instanceof Error && "details" in err ? err.details : null)?.missing;
  const codes = Array.isArray(missing) ? missing.map((m) => m.code) : [codeOf(err)];
  return codes.filter((c) => FIX_LINKS[c]).map((c) => FIX_LINKS[c]);
}

/**
 * @param {unknown} err
 * @returns {{ message: string, code: string, next: string, links: { href: string, text: string }[] }}
 */
export function describeError(err) {
  const code = codeOf(err);
  const message = err instanceof Error && err.message ? err.message : "Something went wrong.";
  return { message, code, next: NEXT_STEP[code] ?? "retry, or check the server log", links: fixLinks(err) };
}
