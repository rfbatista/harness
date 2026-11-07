// Branch requests as the markup binds them: a card per request.

import { count, relativeTime } from "../../../shared/presentation/format.js";

/** "Create branch feature-x", "Switch to commit abc1234", "Rename a to b". */
export function titleOf(r) {
  switch (r.action) {
    case "create":
      return `Create branch ${r.branch}`;
    case "switch":
      return r.branch ? `Switch to branch ${r.branch}` : `Switch to commit ${(r.startPoint?.commit || r.startPoint?.ref || "").slice(0, 8)}`;
    case "rename":
      return `Rename branch ${r.branch} to ${r.newName}`;
    case "delete":
      return `Delete branch ${r.branch}`;
    default:
      return `${r.action} ${r.branch}`.trim();
  }
}

/** "Starts from branch main (abc1234)": what a created or switched-to branch starts from. */
export function startLineOf(r) {
  const s = r.startPoint;
  if (!s) return "";
  const commit = s.commit ? ` (${s.commit.slice(0, 8)})` : "";
  return `Starts from ${s.kind === "branch" ? "branch" : "commit"} ${s.ref}${commit}`;
}

/**
 * @param {import("../domain/request.js").BranchRequest} r
 * @param {{ sessions: Record<string, string>, now: Date }} ctx
 */
export function toCard(r, { sessions, now }) {
  return {
    id: r.id,
    anchor: `branch-request-${r.id}`,
    title: titleOf(r),
    startLine: startLineOf(r),
    sessionName: sessions[r.sessionId] ?? "A session",
    repository: r.repositoryId,
    sessionBranch: r.sessionBranch,
    reason: r.reason,
    age: relativeTime(r.requestedAt, now),
  };
}

/** "1 branch request waits on you", "3 branch requests wait on you". */
export const waitingLine = (n) => `${count(n, "branch request")} ${n === 1 ? "waits" : "wait"} on you`;
