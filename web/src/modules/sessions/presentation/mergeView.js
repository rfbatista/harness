// What the Merge tab shows of a merge offer and a merge result. Pure.

const UNAVAILABLE = {
  MERGE_NO_BRANCH: "This session has no branch to merge.",
  MERGE_NO_CHECKOUT: "No checkout to merge in: this session's repository has no local checkout.",
};

/** The chosen way to merge. Rebase is not one of them. */
export const METHODS = Object.freeze([
  { value: "merge", label: "Merge commit", hint: "Keeps every commit. --no-ff." },
  { value: "squash", label: "Squash", hint: "One commit on the target. The branch keeps its commits." },
]);

const plural = (n, word) => `${n} ${word}${n === 1 ? "" : "s"}`;

/** Why a merge is not offered, in words; "" when it is, or when the form shows the reason beside a disabled button. */
export function unavailableText(offer) {
  if (!offer || offer.available || isBlocked(offer)) return "";
  if (offer.reason === "MERGE_ALREADY_MERGED") return `Nothing to merge: ${offer.branch} has no commits that ${offer.into} lacks.`;
  if (offer.reason === "MERGE_TARGET_NOT_FOUND") return `${offer.into} is not a local branch of this repository. Pick another target.`;
  return UNAVAILABLE[offer.reason] ?? "A merge does not apply to this session.";
}

/** The target is checked out and dirty: the button shows, disabled, with the reason beside it. */
export function isBlocked(offer) {
  return offer?.reason === "MERGE_CHECKOUT_DIRTY";
}

export function blockedText(offer) {
  return isBlocked(offer) ? `${offer.into} is checked out in the main checkout, and it has uncommitted changes. Commit or stash them there, or pick another target.` : "";
}

/** The warning above the form: the session's own worktree has work the merge will not carry. */
export function uncommittedText(offer) {
  const n = offer?.uncommittedChanges ?? 0;
  return n > 0 ? `${plural(n, "file")} in the session's worktree ${n === 1 ? "is" : "are"} not committed. The merge leaves ${n === 1 ? "it" : "them"} out.` : "";
}

/** The button: "Merge into main", "Squash into main", or into the default branch when none is known yet. */
export function mergeLabel(offer, method = "merge") {
  return `${method === "squash" ? "Squash" : "Merge"} into ${offer?.into || "the default branch"}`;
}

/** A target as the closed picker shows it: its name and its marks. */
export function targetLabel(target) {
  return [target.name, target.isDefault ? "default" : "", target.checkedOut ? "checked out" : ""].filter(Boolean).join(" · ");
}

/** "What will happen": what the button does, in the person's git words. */
export function consequence(offer, method = "merge") {
  if (!offer?.available) return "";
  const target = offer.targets.find((t) => t.name === offer.into);
  const commits = plural(offer.ahead, "commit");
  const how =
    method === "squash"
      ? `Squashes ${commits} from ${offer.branch} into one new commit on ${offer.into}. ${offer.branch} keeps its commits.`
      : `Merges ${commits} from ${offer.branch} into ${offer.into} with a merge commit.`;
  const checkout = target?.checkedOut
    ? `${offer.into} is checked out in the main checkout, so its files change there.`
    : "The main checkout is not touched.";
  return `${how} ${checkout} Nothing is pushed.`;
}

const UNCHANGED = "Nothing was pushed. The session, its worktree and the task status are unchanged.";

/**
 * How the newest merge ended: a title and the detail under it, and the tone
 * of the banner. null when there was none.
 * @param {import("../domain/session.js").MergeResult | null} result
 * @returns {{ title: string, detail: string, note: string, code: string, tone: string, role: "status" | "alert" } | null}
 */
export function resultView(result) {
  if (!result) return null;
  const into = result.into || "the default branch";
  if (result.merged) {
    return { title: `Merged into ${into}`, detail: `${into} is now at ${result.head.slice(0, 8)}.`, note: UNCHANGED, code: "", tone: "signal", role: "status" };
  }
  if (result.code === "MERGE_CONFLICT") {
    const files = result.files?.length ? `Conflicts in ${result.files.join(", ")}. ` : `${result.message || "The merge conflicts."} `;
    return {
      title: "Not merged: it conflicts",
      detail: `${files}Nothing changed. Ask the agent to merge ${into} into its branch, then merge again.`,
      note: "",
      code: result.code,
      tone: "danger",
      role: "alert",
    };
  }
  return { title: "Not merged", detail: result.message || "The merge was refused.", note: "", code: result.code, tone: "danger", role: "alert" };
}
