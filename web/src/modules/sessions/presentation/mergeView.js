// What the Merge tab shows of a merge offer and a merge result. Pure.

const UNAVAILABLE = {
  MERGE_NO_BRANCH: "This session has no branch to merge.",
  MERGE_NO_CHECKOUT: "No checkout to merge in: this session's repository has no local checkout.",
};

/** Why a merge is not offered, in words; "" when it is. */
export function unavailableText(offer) {
  if (!offer || offer.available) return "";
  return UNAVAILABLE[offer.reason] ?? "A merge does not apply to this session.";
}

/** The button: "Merge into main", or into the default branch when none is known yet. */
export function mergeLabel(offer) {
  return `Merge into ${offer?.into || "the default branch"}`;
}

/**
 * How the newest merge ended: a title and the detail under it, and the tone
 * of the banner. null when there was none.
 * @param {import("../domain/session.js").MergeResult | null} result
 * @returns {{ title: string, detail: string, code: string, tone: string } | null}
 */
export function resultView(result) {
  if (!result) return null;
  if (result.merged) {
    return {
      title: `Merged into ${result.into || "the default branch"}`,
      detail: `The default branch is now at ${result.head.slice(0, 8)}.`,
      code: "",
      tone: "signal",
    };
  }
  return { title: "Not merged", detail: result.message || "The merge was refused.", code: result.code, tone: "danger" };
}
