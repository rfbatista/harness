// A feature brainstorm: a product-mode session started from the project
// board with no task (ADR 0016). The agent interviews the person, creates the
// feature task, writes its PRD and creates one child task per piece of work.
// Here: what the start request is, and what stops it.

import { INITIAL_TERMINAL_SIZE, PRODUCT_MODE } from "./session.js";

/** Why a brainstorm cannot start, or "" when it can. */
export function brainstormProblem({ repositoryId }) {
  return repositoryId ? "" : "Pick a repository: the session runs in a worktree of it.";
}

/**
 * The start request: product mode, no task, no first message (the interview
 * starts in the terminal), approvals asked in the terminal.
 * @returns {import("./ports.js").StartRequest}
 */
export function brainstormRequest({ projectId, agentId = "", repositoryId }) {
  return {
    projectId,
    ticketId: "",
    repositoryId,
    agentId,
    mode: PRODUCT_MODE,
    prompt: "",
    autoAccept: "off",
    baseBranch: "",
    size: INITIAL_TERMINAL_SIZE,
  };
}
