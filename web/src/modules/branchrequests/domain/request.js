// A branch request as the web client holds it (Glossary "Branch request"; the
// Web UI contract "branch requests and the Branches list" §1): one branch
// action an agent session waits to make, which only the person answers.

/** @typedef {"approve" | "deny"} Decision */

/**
 * @typedef {object} BranchRequest
 * @property {string} id
 * @property {string} sessionId
 * @property {string} taskId
 * @property {string} repositoryId
 * @property {string} sessionBranch
 * @property {string} action  create, switch, rename, delete, push or a remote action
 * @property {string} branch
 * @property {string} newName
 * @property {{ kind: string, ref: string, commit: string } | null} startPoint
 * @property {string} reason
 * @property {"pending" | "approved" | "denied" | "used" | "expired"} state
 * @property {string} note
 * @property {Date} requestedAt
 */

export const STATES = ["pending", "approved", "denied", "used", "expired"];

export const isPending = (r) => r.state === "pending";

/** The request, put into the list in its place: newest first, one per id. */
export function upsertRequest(list, request) {
  const rest = list.filter((r) => r.id !== request.id);
  return [...rest, request].sort((a, b) => b.requestedAt - a.requestedAt || (a.id < b.id ? 1 : -1));
}
