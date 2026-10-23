// Reassembles the final assistant message of a streamed response from its SSE
// events (the Anthropic Messages stream): text, thinking and tool-use blocks in
// order, and the final usage. Pure.

/**
 * @typedef {object} AssembledMessage
 * @property {string} id
 * @property {string} model
 * @property {string} role
 * @property {object[]} content  text, thinking and tool_use blocks, in order
 * @property {string|null} stop_reason
 * @property {object|null} usage  the start usage updated by the final one
 */

const isObject = (v) => v !== null && typeof v === "object";

/**
 * @param {{event: string, data: unknown}[]|null|undefined} events
 * @returns {AssembledMessage|null} null when there are no events
 */
export function reassembleMessage(events) {
  if (!events || events.length === 0) return null;
  const msg = { id: "", model: "", role: "assistant", content: [], stop_reason: null, usage: null };
  const blocks = new Map(); // index -> { block, json }
  for (const { data } of events) {
    if (!isObject(data)) continue;
    switch (data.type) {
      case "message_start": {
        const m = data.message ?? {};
        msg.id = m.id ?? "";
        msg.model = m.model ?? "";
        msg.role = m.role ?? "assistant";
        if (isObject(m.usage)) msg.usage = { ...m.usage };
        break;
      }
      case "content_block_start":
        blocks.set(data.index, { block: { ...data.content_block }, json: "" });
        break;
      case "content_block_delta":
        applyDelta(blocks.get(data.index), data.delta ?? {});
        break;
      case "message_delta":
        if (data.delta?.stop_reason !== undefined) msg.stop_reason = data.delta.stop_reason;
        if (isObject(data.usage)) msg.usage = { ...msg.usage, ...data.usage };
        break;
    }
  }
  msg.content = [...blocks.entries()].sort((a, b) => a[0] - b[0]).map(([, b]) => finishBlock(b));
  return msg;
}

function applyDelta(slot, delta) {
  if (!slot) return;
  if (delta.type === "text_delta") slot.block.text = (slot.block.text ?? "") + delta.text;
  else if (delta.type === "thinking_delta") slot.block.thinking = (slot.block.thinking ?? "") + delta.thinking;
  else if (delta.type === "input_json_delta") slot.json += delta.partial_json;
}

function finishBlock({ block, json }) {
  if (block.type !== "tool_use" || json === "") return block;
  try {
    return { ...block, input: JSON.parse(json) };
  } catch {
    return { ...block, input: null, partial_json: json };
  }
}
