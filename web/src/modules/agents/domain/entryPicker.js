// The keys of the skill-and-bundle picker (UX spec section 6, accessibility):
// arrows move through the options, Enter adds the active one, Backspace in an
// empty input removes the last chip, Escape closes. Pure: the component
// applies what this answers.

/**
 * @param {{ open: boolean, active: number, query: string, chosen: string[] }} state
 * @param {string} key  KeyboardEvent.key
 * @param {string[]} options  the keys of the options shown, in order
 * @returns {{ open: boolean, active: number, handled: boolean, add?: string, removeLast?: boolean }}
 */
export function pickerKey(state, key, options) {
  const n = options.length;
  switch (key) {
    case "ArrowDown":
      return { open: true, active: state.open && n ? (state.active + 1) % n : 0, handled: true };
    case "ArrowUp":
      return { open: true, active: state.open && n ? (state.active - 1 + n) % n : Math.max(n - 1, 0), handled: true };
    case "Enter":
      if (!state.open) return { open: false, active: state.active, handled: false };
      return n ? { open: true, active: state.active, handled: true, add: options[Math.min(state.active, n - 1)] } : { open: true, active: 0, handled: true };
    case "Backspace":
      if (state.query === "" && state.chosen.length > 0) return { open: state.open, active: state.active, handled: true, removeLast: true };
      return { open: state.open, active: state.active, handled: false };
    case "Escape":
      return state.open ? { open: false, active: 0, handled: true } : { open: false, active: state.active, handled: false };
    default:
      return { open: state.open, active: state.active, handled: false };
  }
}
