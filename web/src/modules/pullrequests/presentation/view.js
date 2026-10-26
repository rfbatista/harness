// How pull requests read on screen: state words and tones, the reasons a
// create cannot go ahead, and what a refusal means. Plain functions over
// domain values; the components bind what they return.

import { Codes, codeOf } from "../../../shared/domain/errors.js";
import { describeError } from "../../../shared/presentation/errors.js";
import { displayState } from "../domain/pullRequest.js";

/** The badge block's tone per state; null leaves it neutral. */
const TONES = { open: "success", draft: null, merged: "signal", closed: "danger" };

/** A PR as its chip and badges show it. */
export function toChip(pr) {
  const state = displayState(pr);
  return {
    number: pr.number,
    label: `#${pr.number}`,
    short: `PR #${pr.number}`,
    title: pr.title,
    href: pr.url,
    state,
    tone: TONES[state],
    describe: `Pull request #${pr.number}, ${state}: ${pr.title}. Opens on GitHub.`,
  };
}

/** "Last fetched 3 min ago"; says so when origin's branches were never fetched by the harness. */
export function fetchedText(fetchedAt, now = new Date()) {
  if (!fetchedAt) return "Not fetched yet. The list may be out of date.";
  const minutes = Math.floor((now.getTime() - fetchedAt.getTime()) / 60000);
  if (minutes < 1) return "Last fetched just now";
  if (minutes < 60) return `Last fetched ${minutes} min ago`;
  if (minutes < 24 * 60) return `Last fetched ${Math.floor(minutes / 60)} h ago`;
  return `Last fetched ${fetchedAt.toLocaleDateString()}`;
}

/** What Create does, before it is pressed: the push and the base. */
export function consequenceLine(draft, form) {
  const base = form.baseBranch.trim() || draft.baseBranch;
  return `Pushes ${draft.headBranch} to origin and opens ${form.draft ? "a draft " : "a "}pull request into ${base}.`;
}

const plural = (n, one, many) => `${n} ${n === 1 ? one : many}`;

/** "3 commits from agent/x into main on acme/harness" */
export function summaryLine(draft, base = draft.baseBranch) {
  const where = draft.remote ? ` on ${draft.remote.owner}/${draft.remote.repo}` : "";
  return `${plural(draft.ahead, "commit", "commits")} from ${draft.headBranch} into ${base || draft.baseBranch}${where}`;
}

/** The warning for what will be left out; "" when nothing is. */
export function uncommittedLine(draft) {
  if (draft.uncommittedChanges === 0) return "";
  return `${plural(draft.uncommittedChanges, "uncommitted change", "uncommitted changes")} in the worktree will not be in the pull request.`;
}

/** Why a create cannot go ahead, as the draft says, per blocker code. */
export function blockerText(code, draft) {
  switch (code) {
    case Codes.PR_NO_COMMITS:
      return `Nothing to propose yet: ${draft.headBranch} has no commits ahead of ${draft.baseBranch}.`;
    case Codes.PR_NO_REMOTE:
      return "This repository has no GitHub origin remote, so there is nowhere to open a pull request.";
    case Codes.PR_TOOL_UNAVAILABLE: // retired by the contract's revision 3; an older server may still send it
      return "The server can't reach GitHub right now.";
    case Codes.GIT_CREDENTIAL_MISSING:
      return `No credential stored for ${draft.remote?.host || "this repository's host"} can push this branch or open the pull request. The harness never uses the server machine's git, ssh or gh sign-ins.`;
    case Codes.PR_BASE_NOT_FOUND:
      return `${draft.baseBranch} is not a branch on origin.`;
    case Codes.PR_ALREADY_EXISTS:
      return "A pull request is already open for this branch.";
    default:
      return `The server would refuse it (${code}).`;
  }
}

/** The Git credentials page (the credentials module's; modules do not import each other). */
export const CREDENTIALS_HREF = "/git-credentials";

const credentialsLink = (params = {}) => {
  const q = new URLSearchParams(Object.entries(params).filter(([, v]) => v));
  return q.size ? `${CREDENTIALS_HREF}?${q}` : CREDENTIALS_HREF;
};

/**
 * Where a blocker's fix is, and the link's words; null when it has none.
 * The Go page's BlockerLink says the same.
 * @returns {{ href: string, text: string } | null}
 */
export function blockerLink(code, draft) {
  if (code !== Codes.GIT_CREDENTIAL_MISSING) return null;
  return { href: credentialsLink({ host: draft.remote?.host }), text: "Add one in Git credentials" };
}

/** The server's reason without the code its messages start with. */
export function reasonOf(err) {
  const message = err instanceof Error ? err.message : "";
  const code = codeOf(err);
  return message.startsWith(code) ? message.slice(code.length).replace(/^[:\s]+/, "") : message;
}

const LEADS = {
  [Codes.PR_NO_COMMITS]: "Nothing to propose: origin already has every commit of this branch.",
  [Codes.PR_NO_REMOTE]: "This repository has no GitHub origin remote, so there is nowhere to open a pull request.",
  [Codes.PR_TOOL_UNAVAILABLE]: "The server can't reach GitHub right now.",
  [Codes.GIT_CREDENTIAL_STORE_LOCKED]: "The server can't read its stored credentials: its encryption key is missing or wrong.",
  [Codes.PR_PUSH_FAILED]: "Git refused the push.",
  [Codes.PR_FETCH_FAILED]: "Could not fetch origin.",
  [Codes.PR_CREATE_FAILED]: "GitHub refused the pull request.",
};

/**
 * A refused create as the form's banner shows it: what happened, the server's
 * reason, the code, and for a credential refusal the link to fix it.
 * @returns {{ lead: string, reason: string, code: string, href?: string, linkText?: string }}
 */
export function refusalOf(err) {
  const code = codeOf(err);
  const details = err?.details ?? {};
  const host = details.host || "the host";
  if (code === Codes.GIT_CREDENTIAL_MISSING) {
    const what = details.kind === "ssh_key" ? "SSH key" : "token";
    return { lead: `No ${what} stored for ${host}.`, reason: reasonOf(err), code, href: credentialsLink({ host: details.host, kind: details.kind }), linkText: `Add one in Git credentials` };
  }
  if (code === Codes.GIT_CREDENTIAL_REJECTED) {
    const href = details.credential_id ? credentialsLink({ credential: details.credential_id }) : credentialsLink({ host: details.host });
    return { lead: `${host} refused the stored credential.`, reason: reasonOf(err), code, href, linkText: "Replace it in Git credentials" };
  }
  if (code === Codes.GIT_CREDENTIAL_STORE_LOCKED) return { lead: LEADS[code], reason: reasonOf(err), code, href: CREDENTIALS_HREF, linkText: "See Git credentials" };
  if (LEADS[code]) return { lead: LEADS[code], reason: reasonOf(err), code };
  const d = describeError(err);
  return { lead: d.message, reason: d.next, code: d.code };
}
