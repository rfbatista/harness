// Parses the SSE framing of a streamed response body: "event: <name>" and
// "data: <line>" lines, an event ending at a blank line. Chunks may split
// anywhere, so the parser keeps what it has not finished.

/**
 * @param {(name: string, data: string) => void} onEvent
 */
export function sseParser(onEvent) {
  let buffer = "";
  let name = "";
  /** @type {string[]} */
  let data = [];

  const dispatch = () => {
    if (data.length) onEvent(name || "message", data.join("\n"));
    name = "";
    data = [];
  };

  const line = (text) => {
    if (text === "") return dispatch();
    if (text.startsWith(":")) return;
    const colon = text.indexOf(":");
    const field = colon < 0 ? text : text.slice(0, colon);
    const value = colon < 0 ? "" : text.slice(colon + 1).replace(/^ /, "");
    if (field === "event") name = value;
    else if (field === "data") data.push(value);
  };

  return {
    /** @param {string} chunk */
    push(chunk) {
      buffer += chunk;
      let end;
      while ((end = buffer.search(/\r\n|\n|\r/)) >= 0) {
        // A CR at the very end may be the first half of a CRLF: wait for more.
        if (buffer[end] === "\r" && end === buffer.length - 1) return;
        const eol = buffer.startsWith("\r\n", end) ? 2 : 1;
        line(buffer.slice(0, end));
        buffer = buffer.slice(end + eol);
      }
    },
    /** The stream ended: a line cut off before its newline is dropped; an event whose lines are all complete still counts. */
    end() {
      buffer = "";
      dispatch();
    },
  };
}
