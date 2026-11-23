// The colour an agent has everywhere: the same name, the same slot.

/** The colour slots an agent chip picks from; the stylesheet has one colour for each. */
export const AGENT_SLOTS = 8;

/** The slot of an agent's colour, the same for the same name. */
export function agentSlot(name) {
  let h = 0;
  for (const c of String(name ?? "")) h = (h * 31 + c.codePointAt(0)) >>> 0;
  return h % AGENT_SLOTS;
}
