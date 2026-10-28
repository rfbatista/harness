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
import { lineAnchor } from "../../domain/diff.js";
import { diffRows, fileRows, tokenise } from "../../domain/rows.js";

export { diffRows, fileRows };

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
 * @param {import("../../domain/rows.js").Row} row
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
 * @param {import("../../domain/rows.js").Row[]} opts.rows
 * @param {HTMLElement|null} opts.scroller  what scrolls (the code view's .scroll, or the diff's main side)
 * @param {string} [opts.slug]               a diff file's slug, for its line anchors
 * @param {import("../../domain/ports.js").Highlighter} [opts.highlighter]
 * @param {string} [opts.language]
 * @param {boolean} [opts.virtual]           allow windowing (no wrap, no split)
 * @param {import("../../domain/lines.js").LineRange|null} [opts.target]
 * @param {"R"|"L"} [opts.targetSide]
 * @param {(cb: () => void) => void} [opts.idle]  when to highlight (tests run it at once)
 * @param {boolean} [opts.deferred]          draw nothing yet: the owner calls step() until it says done, or finish()
 */
export function drawLines(el, { rows, scroller = null, slug = "", highlighter = null, language = "plaintext", virtual = true, target = null, targetSide = "R", idle = defaultIdle, deferred = false }) {
  const doc = el.ownerDocument;
  let tokens = new Map();
  const ctx = {
    slug,
    target,
    targetSide,
    tokens: (row) => row.tokens ?? tokens.get(row) ?? null,
  };
  const windowed = virtual && rows.length > VIRTUALISE_FROM && scroller !== null;
  let tops = null; // prefix sums of row heights, windowed only
  let shown = { first: -1, last: -1 };
  let frame = 0;
  let destroyed = false;

  // Rows drawn so far when not windowed; a deferred block fills in slices.
  let next = 0;

  function drawAll() {
    next = rows.length;
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

  function startWindow() {
    next = rows.length;
    measure();
    drawWindow(true);
    scroller.addEventListener("scroll", onScroll, { passive: true });
  }

  if (!deferred) {
    if (windowed) startWindow();
    else drawAll();
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
    /** Whether every row is drawn (a windowed block is, once its window is). */
    get complete() {
      return next >= rows.length;
    },
    /**
     * Draws the next `count` rows of a deferred block, in order; true when
     * nothing is left. A windowed block draws its window in one step.
     */
    step(count) {
      if (destroyed || next >= rows.length) return true;
      if (windowed) {
        startWindow();
        return true;
      }
      const end = Math.min(rows.length, next + count);
      const slice = rows.slice(next, end).map((r) => rowElement(doc, r, ctx));
      if (next === 0) el.replaceChildren(...slice);
      else el.append(...slice);
      next = end;
      return next >= rows.length;
    },
    /** Draws what is left at once (a link or a key asked for this block). */
    finish() {
      if (destroyed || next >= rows.length) return;
      if (windowed) startWindow();
      else drawAll();
    },
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
      if (deferred && next === 0) return; // the first rows are drawn with the target
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
 * Fills a .code-lines with a skeleton block of `rowCount` rows, and its
 * .hunks with aria-busy: the block keeps the height its rows will have (one
 * code line each) so the scrollbar is true before they are drawn. It is one
 * element, however many rows: hundreds of sections wait at once. The first rows
 * drawn replace it.
 * @param {HTMLElement} el
 */
export function showPending(el, rowCount) {
  const doc = el.ownerDocument;
  const holder = doc.createElement("div");
  holder.className = "skeleton-lines";
  holder.setAttribute("aria-hidden", "true");
  holder.style.setProperty("--rows", String(rowCount));
  el.replaceChildren(holder);
  el.closest(".hunks")?.setAttribute("aria-busy", "true");
}

/** The rows are drawn: the section is no longer busy. */
export function clearPending(el) {
  el.closest(".hunks")?.removeAttribute("aria-busy");
}

function defaultIdle(cb) {
  if (typeof requestIdleCallback === "function") requestIdleCallback(() => cb(), { timeout: 500 });
  else setTimeout(cb, 0);
}
