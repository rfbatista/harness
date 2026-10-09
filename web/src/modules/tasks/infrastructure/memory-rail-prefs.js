// RailPrefs in memory, for tests; it obeys the same contract as the cookie
// (../testing/rail-prefs-contract.js).

/** @returns {import("../domain/ports.js").RailPrefs} */
export function memoryRailPrefs(initial = []) {
  let keys = [...initial];
  return {
    collapsed: () => [...keys],
    setCollapsed(next) {
      keys = [...new Set(next)];
    },
  };
}
