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
    case "push":
      return `Push branch ${r.branch}`;
    case "remote_add":
      return `Add remote ${r.remote?.name ?? ""}`;
    case "remote_set_url":
      return r.branch ? `Set the remote of branch ${r.branch} to ${r.remote?.name ?? ""}` : `Change the URL of remote ${r.remote?.name ?? ""}`;
    case "remote_rename":
      return `Rename remote ${r.remote?.name ?? ""} to ${r.remote?.newName ?? ""}`;
    case "remote_remove":
      return `Remove remote ${r.remote?.name ?? ""}`;
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
 * "To origin (git@host:acme/x.git) as review/x · 3 commits" for a push, "New URL …" for a remote that gets one:
 * where the push goes, or what the remote becomes. The URL is shown as configured (credentials in it are redacted
 * by the server).
 */
export function detailLineOf(r) {
  if (r.action === "push" && r.remote) {
    const where = `To ${r.remote.name} (${r.remote.url}) as ${r.remoteBranch || r.branch}`;
    return r.commits === null ? where : `${where} · ${count(r.commits, "commit")}`;
  }
  if ((r.action === "remote_add" || r.action === "remote_set_url") && r.remote?.url) return `New URL ${r.remote.url}`;
  return "";
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
    detailLine: detailLineOf(r),
    sessionName: sessions[r.sessionId] ?? "A session",
    repository: r.repositoryId,
    sessionBranch: r.sessionBranch,
    reason: r.reason,
    age: relativeTime(r.requestedAt, now),
  };
}

/** "1 branch request waits on you", "3 branch requests wait on you". */
export const waitingLine = (n) => `${count(n, "branch request")} ${n === 1 ? "waits" : "wait"} on you`;
