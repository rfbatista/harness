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
    case Codes.PR_TOOL_UNAVAILABLE:
      return "The server can't reach GitHub as you: gh is missing or not signed in on the server machine. Run gh auth login there.";
    case Codes.PR_BASE_NOT_FOUND:
      return `${draft.baseBranch} is not a branch on origin.`;
    case Codes.PR_ALREADY_EXISTS:
      return "A pull request is already open for this branch.";
    default:
      return `The server would refuse it (${code}).`;
  }
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
  [Codes.PR_TOOL_UNAVAILABLE]: "The server can't reach GitHub as you.",
  [Codes.PR_PUSH_FAILED]: "Git refused the push.",
  [Codes.PR_CREATE_FAILED]: "GitHub refused the pull request.",
};

/**
 * A refused create as the form's banner shows it: what happened, the server's
 * reason (for the tool, what to run), and the code.
 * @returns {{ lead: string, reason: string, code: string }}
 */
export function refusalOf(err) {
  const code = codeOf(err);
  if (LEADS[code]) return { lead: LEADS[code], reason: reasonOf(err), code };
  const d = describeError(err);
  return { lead: d.message, reason: d.next, code: d.code };
}
