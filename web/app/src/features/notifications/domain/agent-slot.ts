/** The colour slots an agent chip picks from; the stylesheet has one colour for each. */
export const AGENT_SLOTS = 8;

/** The slot of an agent's colour, the same for the same name everywhere (the Alpine client's rule). */
export function agentSlot(name: string): number {
  let h = 0;
  for (const c of String(name)) h = (h * 31 + (c.codePointAt(0) ?? 0)) >>> 0;
  return h % AGENT_SLOTS;
}
