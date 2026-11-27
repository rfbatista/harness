// The Guard line a repository carries on the pages that list it (Repositories,
// project Settings): red with the required gate kinds that have no command at
// any level, neutral when none is missing. It reads the repository's
// agent-system view, the one the Agent system page reads.

/** @typedef {{ state: "loading" | "ok" | "red", text: string, missing: string[] }} GuardLine */

const words = (kind) => String(kind).replaceAll("_", " ");

/** @returns {GuardLine} */
export const loadingLine = () => ({ state: "loading", text: "", missing: [] });

/**
 * @param {unknown} view the repository's agent-system answer
 * @returns {GuardLine}
 */
export function guardLine(view) {
  const effective = /** @type {{ effective?: { missing_required_gates?: string[] } } | null} */ (view)?.effective;
  if (!effective || typeof effective !== "object") throw new Error("not an agent-system view");
  const missing = (effective.missing_required_gates ?? []).map(words);
  if (missing.length === 0) return { state: "ok", text: "Guard: every required gate has a command", missing };
  return { state: "red", text: `${missing.join(", ")} missing`, missing };
}
