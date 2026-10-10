// The one imperative piece of the code views: the rows of a .code-lines block
// (contract §1, code-lines). Built with createElement and textContent, never
// innerHTML, so file content is only ever text. Alpine owns everything around
// it; an x-for over 10,000 lines with token spans is too slow on the CSP build.
//
// Above VIRTUALISE_FROM rows (and only without wrap or split, whose rows grow)
// just the window around the scroller's viewport is in the DOM, between two
// aria-hidden spacers. Every line is exactly --size-code-line high and a hunk
// row a fixed amount taller, both read once from rendered rows, so a row's
// top is a prefix sum, never a measurement per row.
//
// Rows are highlighted after paint: a Highlighter tokenises each side's text
// once, and the rows on screen are redrawn with their tokens.

import { VIRTUALISE_FROM, inRange, windowOf } from "../../domain/lines.js";
import { lineAnchor, sidesOf, splitRows } from "../../domain/diff.js";

/**
 * @typedef {{ type: "line", n: number, text: string }} FileRow
 * @typedef {{ type: "hunk", range: string, header: string }} HunkRow
 * @typedef {{ type: "diff", kind: string, old: number|null, new: number|null, text: string, noNewline: boolean, tokens?: any }} DiffRow
 * @typedef {{ type: "split", left: DiffRow|null, right: DiffRow|null }} SplitRow
 * @typedef {FileRow|HunkRow|DiffRow|SplitRow} Row
 */

/** Rows of a file: one per line, numbered from 1. */
export function fileRows(lines) {
  return lines.map((text, i) => ({ type: "line", n: i + 1, text }));
}

const rangeOf = (h) => `@@ -${h.oldStart},${h.oldLines} +${h.newStart},${h.newLines} @@`;

/** Rows of a file diff's hunks, unified or split. */
export function diffRows(hunks, layout = "unified") {
  const rows = [];
  for (const h of hunks ?? []) {
    rows.push({ type: "hunk", range: rangeOf(h), header: h.header });
    if (layout === "split") {
      for (const { left, right } of splitRows(h.lines)) {
        const l = left && toDiffRow(left);
        // A context line is one row on both sides, so it is highlighted once.
        rows.push({ type: "split", left: l, right: right === left ? l : right && toDiffRow(right) });
      }
    } else {
      for (const l of h.lines) rows.push(toDiffRow(l));
    }
  }
  return rows;
}

const toDiffRow = (l) => ({ type: "diff", kind: l.kind, old: l.old, new: l.new, text: l.text, noNewline: l.noNewline });

// ── row elements ─────────────────────────────────────────────────────────

function textSpan(doc, text, tokens) {
  const span = doc.createElement("span");
  span.className = "text";
  if (!tokens) {
    span.textContent = text;
    return span;
  }
  for (const t of tokens) {
    if (!t.tok) {
      span.append(doc.createTextNode(t.text));
      continue;
    }
    const tok = doc.createElement("span");
    tok.className = `tok-${t.tok}`;
    tok.textContent = t.text;
    span.append(tok);
  }
  return span;
}

function num(doc, n, href, label) {
  if (n === null || n === undefined) {
    const empty = doc.createElement("span");
    empty.className = "num";
    return empty;
  }
  const a = doc.createElement("a");
  a.className = "num";
  a.dataset.line = String(n);
  a.href = `#${href}`;
  a.tabIndex = -1;
  a.setAttribute("aria-label", `${label} ${n}`);
  return a;
}

function gutter(doc, ...nums) {
  const g = doc.createElement("span");
  g.className = "gutter";
  g.append(...nums);
  return g;
}

/**
 * @param {Document} doc
 * @param {Row} row
 * @param {{ slug: string, tokens: (row: Row) => any, target: import("../../domain/lines.js").LineRange|null, targetSide: string }} ctx
 */
function rowElement(doc, row, ctx) {
  if (row.type === "hunk") {
    const div = doc.createElement("div");
    div.className = "hunk";
    const range = doc.createElement("span");
    range.className = "range";
    range.textContent = row.range;
    div.append(range);
    if (row.header) {
      const c = doc.createElement("span");
      c.className = "context";
      c.textContent = row.header;
      div.append(c);
    }
    return div;
  }
  const div = doc.createElement("div");
  div.className = "line";
  if (row.type === "line") {
    div.id = `L${row.n}`;
    div.append(gutter(doc, num(doc, row.n, `L${row.n}`, "Line")), textSpan(doc, row.text, ctx.tokens(row)));
    if (inRange(ctx.target, row.n)) div.dataset.target = "";
    return div;
  }
  if (row.type === "diff") {
    div.dataset.kind = row.kind;
    if (row.noNewline) div.dataset.noNewline = "";
    div.append(
      gutter(doc, num(doc, row.old, lineAnchor(ctx.slug, "L", row.old), "Old line"), num(doc, row.new, lineAnchor(ctx.slug, "R", row.new), "New line")),
      textSpan(doc, row.text, ctx.tokens(row)),
    );
    if (targets(ctx, row)) div.dataset.target = "";
    return div;
  }
  // split: two halves, each with its own number
  for (const [side, half] of [["left", row.left], ["right", row.right]]) {
    const cell = doc.createElement("div");
    cell.className = "half";
    if (!half) {
      cell.dataset.kind = "empty";
      div.append(cell);
      continue;
    }
    cell.dataset.kind = half.kind;
    if (half.noNewline) cell.dataset.noNewline = "";
    const n = side === "left" ? half.old : half.new;
    cell.append(gutter(doc, num(doc, n, lineAnchor(ctx.slug, side === "left" ? "L" : "R", n), side === "left" ? "Old line" : "New line")), textSpan(doc, half.text, ctx.tokens(half)));
    div.append(cell);
  }
  if ((row.left && targets(ctx, row.left)) || (row.right && targets(ctx, row.right))) div.dataset.target = "";
  return div;
}

function targets(ctx, row) {
  if (!ctx.target) return false;
  const n = ctx.targetSide === "L" ? row.old : row.new;
  return n !== null && inRange(ctx.target, n);
}

// ── the renderer ─────────────────────────────────────────────────────────

/**
 * Draws rows into a .code-lines element and keeps them drawn as its scroller
 * scrolls.
 * @param {HTMLElement} el  the .code-lines element (empty; the caller sets --digits and data attributes)
 * @param {object} opts
 * @param {Row[]} opts.rows
 * @param {HTMLElement|null} opts.scroller  what scrolls (the code view's .scroll, or the diff's main side)
 * @param {string} [opts.slug]               a diff file's slug, for its line anchors
 * @param {import("../../domain/ports.js").Highlighter} [opts.highlighter]
 * @param {string} [opts.language]
 * @param {boolean} [opts.virtual]           allow windowing (no wrap, no split)
 * @param {import("../../domain/lines.js").LineRange|null} [opts.target]
 * @param {"R"|"L"} [opts.targetSide]
 * @param {(cb: () => void) => void} [opts.idle]  when to highlight (tests run it at once)
 */
export function drawLines(el, { rows, scroller = null, slug = "", highlighter = null, language = "plaintext", virtual = true, target = null, targetSide = "R", idle = defaultIdle }) {
  const doc = el.ownerDocument;
  let tokens = new Map();
  const ctx = {
    slug,
    target,
    targetSide,
    tokens: (row) => tokens.get(row) ?? null,
  };
  const windowed = virtual && rows.length > VIRTUALISE_FROM && scroller !== null;
  let tops = null; // prefix sums of row heights, windowed only
  let shown = { first: -1, last: -1 };
  let frame = 0;
  let destroyed = false;

  function drawAll() {
    el.replaceChildren(...rows.map((r) => rowElement(doc, r, ctx)));
  }

  function measure() {
    // One line and (if any) one hunk row, drawn and measured once.
    const probe = [rows.find((r) => r.type !== "hunk"), rows.find((r) => r.type === "hunk")].filter(Boolean).map((r) => rowElement(doc, r, ctx));
    el.replaceChildren(...probe);
    const lineH = probe[0]?.getBoundingClientRect().height || 20;
    const hunkH = rows.some((r) => r.type === "hunk") ? probe.at(-1).getBoundingClientRect().height || lineH : lineH;
    tops = new Float64Array(rows.length + 1);
    for (let i = 0; i < rows.length; i += 1) tops[i + 1] = tops[i] + (rows[i].type === "hunk" ? hunkH : lineH);
    return lineH;
  }

  function offsetInScroller() {
    const a = el.getBoundingClientRect();
    const b = scroller.getBoundingClientRect();
    return a.top - b.top + scroller.scrollTop;
  }

  /** The first row whose bottom is below y (binary search over tops). */
  function rowAt(y) {
    let lo = 0;
    let hi = rows.length;
    while (lo < hi) {
      const mid = (lo + hi) >> 1;
      if (tops[mid + 1] <= y) lo = mid + 1;
      else hi = mid;
    }
    return lo;
  }

  function drawWindow(force = false) {
    frame = 0;
    if (destroyed) return;
    const offset = offsetInScroller();
    const lineH = tops[1] - tops[0] || 20;
    const top = Math.max(0, scroller.scrollTop - offset);
    const first0 = rowAt(top);
    const visible = Math.ceil(scroller.clientHeight / lineH);
    const { first, last } = windowOf({ count: rows.length, rowHeight: 1, scrollTop: first0, viewport: visible });
    if (!force && first === shown.first && last === shown.last) return;
    shown = { first, last };
    const before = doc.createElement("div");
    before.setAttribute("aria-hidden", "true");
    before.style.blockSize = `${tops[first]}px`;
    const after = doc.createElement("div");
    after.setAttribute("aria-hidden", "true");
    after.style.blockSize = `${tops[rows.length] - tops[last]}px`;
    el.replaceChildren(before, ...rows.slice(first, last).map((r) => rowElement(doc, r, ctx)), after);
  }

  const onScroll = () => {
    if (!frame) frame = requestAnimationFrame(() => drawWindow());
  };

  function redraw() {
    if (windowed) drawWindow(true);
    else drawAll();
  }

  if (windowed) {
    measure();
    drawWindow(true);
    scroller.addEventListener("scroll", onScroll, { passive: true });
  } else {
    drawAll();
  }

  // Highlight after paint, then redraw what is on screen.
  if (highlighter?.supports(language)) {
    idle(() => {
      if (destroyed) return;
      tokens = tokenise(rows, highlighter, language);
      redraw();
    });
  }

  return {
    windowed,
    get rowCount() {
      return rows.length;
    },
    /** The rows currently in the DOM (all of them unless windowed). */
    get rendered() {
      return windowed ? shown.last - shown.first : rows.length;
    },
    /** The px offset of row i from the top of the block. */
    offsetOf(i) {
      if (tops) return tops[Math.max(0, Math.min(rows.length, i))];
      const child = el.children[i];
      return child ? child.offsetTop - el.offsetTop : 0;
    },
    /** The px offset of the block inside its scroller. */
    blockOffset() {
      return scroller ? offsetInScroller() : 0;
    },
    setTarget(range, side = "R") {
      ctx.target = range;
      ctx.targetSide = side;
      redraw();
    },
    /** Redraws the window after the scroller moved programmatically. */
    refresh() {
      if (windowed) drawWindow(true);
    },
    destroy() {
      destroyed = true;
      if (frame) cancelAnimationFrame(frame);
      scroller?.removeEventListener("scroll", onScroll);
    },
  };
}

/**
 * Tokens per row: a file's lines in one pass; a diff's hunks per side (the
 * new side for context and additions, the old side for deletions).
 * @returns {Map<object, any>}
 */
function tokenise(rows, highlighter, language) {
  const out = new Map();
  if (rows.every((r) => r.type === "line")) {
    const lines = highlighter.tokenize(language, rows.map((r) => r.text).join("\n"));
    rows.forEach((r, i) => out.set(r, lines[i] ?? null));
    return out;
  }
  // Group the diff rows back into hunks to highlight each side as text.
  let hunk = [];
  const flush = () => {
    if (!hunk.length) return;
    const lines = hunk.map((d) => ({ kind: d.kind, text: d.text }));
    const { newText, oldText, at } = sidesOf({ lines });
    const newT = highlighter.tokenize(language, newText);
    const oldT = highlighter.tokenize(language, oldText);
    hunk.forEach((d, i) => out.set(d, d.kind === "del" ? oldT[at[i].old] : newT[at[i].new]));
    hunk = [];
  };
  for (const r of rows) {
    if (r.type === "hunk") flush();
    else if (r.type === "diff") hunk.push(r);
    else if (r.type === "split") {
      // Old side then new side, in order, without repeating a context line.
      if (r.left) hunk.push(r.left);
      if (r.right && r.right !== r.left) hunk.push(r.right);
    }
  }
  flush();
  return out;
}

function defaultIdle(cb) {
  if (typeof requestIdleCallback === "function") requestIdleCallback(() => cb(), { timeout: 500 });
  else setTimeout(cb, 0);
}
