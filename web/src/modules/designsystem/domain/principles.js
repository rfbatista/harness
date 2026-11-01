// The Principles tab's model: the `##` sections of the Markdown, which the
// outline lists and the server's change summary counts. Pure.

/**
 * @typedef {{ key: string, title: string, line: number, offset: number }} Section
 * `line` is the zero-based line the heading is on, `offset` the character it starts at.
 */

const FENCE = /^\s*(```|~~~)/;
const HEADING = /^## +(.*?)(?: +#+)?\s*$/;

/**
 * The `##` sections of a Markdown text, in order. A `##` line inside a fenced
 * code block is code, not a section; an empty heading names nothing and is skipped.
 * @returns {Section[]}
 */
export function outlineOf(markdown) {
  const out = [];
  let fenced = false;
  let offset = 0;
  (markdown ?? "").split("\n").forEach((raw, line) => {
    const text = raw.replace(/\r$/, "");
    if (FENCE.test(text)) fenced = !fenced;
    else if (!fenced) {
      const m = HEADING.exec(text);
      if (m && m[1].trim() !== "") out.push({ key: `${out.length}:${m[1].trim()}`, title: m[1].trim(), line, offset });
    }
    offset += raw.length + 1;
  });
  return out;
}
