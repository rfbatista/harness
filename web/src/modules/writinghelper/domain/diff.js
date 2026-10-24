// The Changes view: a word diff between the field's text and the draft, so
// the person sees what the helper added (ins) and removed (del). Open
// placeholders ("[open: what is missing]") are single units, shown as gaps.

/** @typedef {{ type: "eq" | "ins" | "del", text: string, gap?: boolean }} Segment */

const GAP = /\[open:[^\]]*\]/g;

// Past this many cells the exact diff is not worth the wait; the whole text
// is replaced instead.
const MAX_CELLS = 4_000_000;

/** @param {string} text @returns {{ text: string, gap: boolean }[]} */
function tokens(text) {
  const out = [];
  let last = 0;
  const push = (chunk) => {
    for (const m of chunk.matchAll(/\s+|\S+/g)) out.push({ text: m[0], gap: false });
  };
  for (const m of text.matchAll(GAP)) {
    push(text.slice(last, m.index));
    out.push({ text: m[0], gap: true });
    last = m.index + m[0].length;
  }
  push(text.slice(last));
  return out;
}

/** @param {Segment[]} out @param {Segment["type"]} type @param {{ text: string, gap: boolean }} token */
function add(out, type, token) {
  const prev = out.at(-1);
  if (prev && prev.type === type && !prev.gap && !token.gap) {
    prev.text += token.text;
    return;
  }
  out.push(token.gap ? { type, text: token.text, gap: true } : { type, text: token.text });
}

/**
 * @param {string} before the field's text
 * @param {string} after the draft
 * @returns {Segment[]} in reading order; a removal comes before its insertion
 */
export function wordDiff(before, after) {
  const a = tokens(before);
  const b = tokens(after);
  const same = (x, y) => x.text === y.text;

  let start = 0;
  while (start < a.length && start < b.length && same(a[start], b[start])) start++;
  let endA = a.length;
  let endB = b.length;
  while (endA > start && endB > start && same(a[endA - 1], b[endB - 1])) {
    endA--;
    endB--;
  }

  /** @type {Segment[]} */
  const out = [];
  for (const t of a.slice(0, start)) add(out, "eq", t);

  const midA = a.slice(start, endA);
  const midB = b.slice(start, endB);
  const n = midA.length;
  const m = midB.length;
  if (n * m > MAX_CELLS) {
    for (const t of midA) add(out, "del", t);
    for (const t of midB) add(out, "ins", t);
  } else {
    // lcs[i][j] is the longest common run of midA[i:] and midB[j:].
    const w = m + 1;
    const lcs = new Uint32Array((n + 1) * w);
    for (let i = n - 1; i >= 0; i--) {
      for (let j = m - 1; j >= 0; j--) {
        lcs[i * w + j] = same(midA[i], midB[j]) ? lcs[(i + 1) * w + j + 1] + 1 : Math.max(lcs[(i + 1) * w + j], lcs[i * w + j + 1]);
      }
    }
    let i = 0;
    let j = 0;
    // Removals are held back and written before the insertions that follow,
    // so a replaced word reads "del ins".
    while (i < n || j < m) {
      if (i < n && j < m && same(midA[i], midB[j])) {
        add(out, "eq", midA[i]);
        i++;
        j++;
      } else if (i < n && (j === m || lcs[(i + 1) * w + j] >= lcs[i * w + j + 1])) {
        add(out, "del", midA[i++]);
      } else {
        add(out, "ins", midB[j++]);
      }
    }
  }

  for (const t of a.slice(endA)) add(out, "eq", t);
  return out;
}

const ESCAPES = { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" };
const escapeHtml = (s) => s.replace(/[&<>"]/g, (c) => ESCAPES[c]);

/** Text with its open placeholders wrapped as gaps, as HTML. */
function withGaps(text) {
  let out = "";
  let last = 0;
  for (const m of text.matchAll(GAP)) {
    out += escapeHtml(text.slice(last, m.index)) + `<span class="gap">${escapeHtml(m[0])}</span>`;
    last = m.index + m[0].length;
  }
  return out + escapeHtml(text.slice(last));
}

/**
 * The draft element's innerHTML: `ins`, `del` and `.gap`, text escaped. The
 * CSS (.draft[data-view="clean"]) hides the removals for the Clean view.
 * @param {Segment[]} segments
 */
export function diffHtml(segments) {
  let out = "";
  for (let i = 0; i < segments.length; ) {
    const type = segments[i].type;
    let html = "";
    for (; i < segments.length && segments[i].type === type; i++) html += withGaps(segments[i].text);
    out += type === "ins" ? `<ins>${html}</ins>` : type === "del" ? `<del>${html}</del>` : html;
  }
  return out;
}
