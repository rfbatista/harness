// The context strip as the page's address names it: a link to a band opens
// its panel for that visit.

/**
 * The panel a URL fragment points at, or "" when it points at none:
 * #reviews and #review-{id} the Reviews panel, #feature the Feature panel,
 * #token-savers the Token savers panel.
 * @param {string} hash
 */
export function bandOfHash(hash) {
  const target = hash.replace(/^#/, "");
  if (target === "reviews" || target.startsWith("review-")) return "reviews";
  if (target === "feature" || target === "token-savers") return target;
  return "";
}

/**
 * Where focus goes from the button at `from` of `count` for a key: Left and
 * Right step (stopping at the ends), Home and End jump. -1 for any other key.
 * @param {number} count
 * @param {number} from
 * @param {string} key
 */
export function buttonToFocus(count, from, key) {
  if (key === "ArrowRight") return Math.min(from + 1, count - 1);
  if (key === "ArrowLeft") return Math.max(from - 1, 0);
  if (key === "Home") return 0;
  if (key === "End") return count - 1;
  return -1;
}
