// A task's or session's totals as one short line ("11K tokens · $0.42
// estimated"), with the tokens by kind and the cost's caveat on hover.
// Formatted like the Usage page (domain/format.js).

import { costView, estimatedCost, tokenCount, tokens } from "../domain/format.js";

/** @param {import("../domain/usage.js").Totals|null|undefined} t */
export function totalsChip(t) {
  if (!t) return null;
  const cost = costView(t);
  const kinds = `${tokenCount(t.input)} input · ${tokenCount(t.output)} output · ${tokenCount(t.cacheWrite)} cache write · ${tokenCount(t.cacheRead)} cache read tokens`;
  const caveat = t.costUsd === null ? "Estimated cost: none, no price for these models" : `Estimated cost ${estimatedCost(t.costUsd)}${cost.note ? ` (${cost.note.toLowerCase()})` : ""}`;
  return {
    text: `${tokens(t.total)} tokens · ${cost.text} estimated`,
    title: `${kinds}. ${caveat}.`,
  };
}
