// How token counts and estimated costs read, on the Usage page and wherever a
// task or session shows its totals. An estimated cost is always labelled
// "estimated" (the Glossary's "Estimated cost"): on a subscription nothing is
// billed per token. A cost the price table cannot give is a dash, never $0.

const UNITS = ["", "K", "M", "B", "T"];

/** "999", "1.2K", "12K", "1.5M": compact, for tiles, bars and rows. */
export function tokens(n) {
  let value = Math.max(0, n);
  let unit = 0;
  while (value >= 1000 && unit < UNITS.length - 1) {
    value /= 1000;
    unit++;
  }
  if (unit === 0) return String(Math.round(value));
  let shown = value < 10 ? Math.round(value * 10) / 10 : Math.round(value);
  if (shown >= 1000 && unit < UNITS.length - 1) {
    shown = 1;
    unit++;
  }
  return `${shown}${UNITS[unit]}`;
}

/** "1,234,567": the exact count, for titles and screen readers. */
export const tokenCount = (n) => Math.round(n).toLocaleString("en-US");

const dollars = new Intl.NumberFormat("en-US", { style: "currency", currency: "USD", minimumFractionDigits: 2, maximumFractionDigits: 2 });

/** "$0.42", "<$0.01", or "—" when nothing was priced (null). */
export function estimatedCost(usd) {
  if (usd === null || usd === undefined) return "—";
  if (usd > 0 && usd < 0.01) return "<$0.01";
  return dollars.format(usd);
}

export const COST_LABEL = "Estimated cost";

/**
 * A set of totals' cost as a tile shows it: the amount, its label, what it
 * leaves out, and what a screen reader hears.
 * @param {import("./usage.js").Totals} totals
 */
export function costView(totals) {
  const text = estimatedCost(totals.costUsd);
  let note = "";
  if (totals.costUsd === null) note = "No price for these models";
  else if (totals.unpriced > 0) note = `Leaves out ${tokens(totals.unpriced)} unpriced tokens`;
  const aria = totals.costUsd === null ? "Estimated cost: none, no price for these models" : `${text} estimated`;
  return { text, label: COST_LABEL, note, aria };
}
