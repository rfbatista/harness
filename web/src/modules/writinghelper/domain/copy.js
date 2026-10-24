// What the panel says, by field kind.

/** The question the panel asks when the field is empty. */
export function startQuestion(kind) {
  return kind === "comment" ? "What do you want to say?" : "What is this task about?";
}

/** The line above the start question. */
export function startLead(kind, noun, siblingLabel) {
  const from = siblingLabel ? `, or start from the ${siblingLabel}` : "";
  return kind === "comment"
    ? `The ${noun} is empty. Say in a few words what you want to tell the agents${from}.`
    : `The ${noun} is empty. Say in a few words what this task is about${from}.`;
}

export function startPlaceholder(kind) {
  return kind === "comment" ? "e.g. keep the pen visible, not only on hover" : "e.g. let people get AI help writing task text";
}

/** The toast after Use this. */
export function usedMessage(noun) {
  const name = noun.charAt(0).toUpperCase() + noun.slice(1);
  return `${name} replaced by the helper's draft`;
}

/** The toast after a title is picked. */
export function pickedMessage() {
  return "Title replaced by the helper's pick";
}

/** What each part of the panel is called when the pen has no better label. */
export const REFINE_CHIPS = ["Shorter", "More detail", "Add acceptance criteria", "Fix grammar only"];
export const LIGHT_EDIT_CHIPS = ["Fix grammar only", "Shorter", "Add acceptance criteria"];
