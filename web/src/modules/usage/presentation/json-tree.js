// A JSON value as indented, coloured DOM: each non-empty object and array is
// a <details> that folds on its own, open to start with. Strings are text
// nodes, so nothing in a call's JSON is ever read as markup. With a search,
// each match is a <mark>, and only the folds on the way to a match are open.

/**
 * @param {Document} doc
 * @param {unknown} value
 * @param {{ query?: string }} [options]
 * @returns {DocumentFragment}
 */
export function jsonTree(doc, value, { query = "" } = {}) {
  const ctx = { doc, q: query.trim().toLowerCase() };
  const frag = doc.createDocumentFragment();
  frag.append(node(ctx, value, false, true));
  return frag;
}

function span(ctx, cls, text) {
  const el = ctx.doc.createElement("span");
  el.className = cls;
  el.textContent = text;
  return el;
}

/** A span of text whose matches of the search are marked. */
function marked(ctx, cls, text) {
  const el = ctx.doc.createElement("span");
  el.className = cls;
  const lower = text.toLowerCase();
  let at = 0;
  for (let i = ctx.q ? lower.indexOf(ctx.q) : -1; i >= 0; i = lower.indexOf(ctx.q, at)) {
    if (i > at) el.append(text.slice(at, i));
    const mark = ctx.doc.createElement("mark");
    mark.className = "json-match";
    mark.textContent = text.slice(i, i + ctx.q.length);
    el.append(mark);
    at = i + ctx.q.length;
  }
  if (at < text.length) el.append(text.slice(at));
  return el;
}

/** Whether the search appears in value, keys included, as the tree shows it. */
function holds(ctx, value) {
  if (!ctx.q) return true;
  if (Array.isArray(value)) return value.some((v) => holds(ctx, v));
  if (value !== null && typeof value === "object") {
    return Object.entries(value).some(([k, v]) => JSON.stringify(k).toLowerCase().includes(ctx.q) || holds(ctx, v));
  }
  return leafText(value).toLowerCase().includes(ctx.q);
}

/** value's nodes, then the comma that follows it in its parent, if any. */
function node(ctx, value, comma = false, root = false) {
  if (Array.isArray(value)) return fold(ctx, "[", "]", value.map((v) => [null, v]), value.length === 1 ? "1 item" : `${value.length} items`, comma, root || holds(ctx, value));
  if (value !== null && typeof value === "object") {
    const entries = Object.entries(value);
    return fold(ctx, "{", "}", entries, entries.length === 1 ? "1 key" : `${entries.length} keys`, comma, root || holds(ctx, value));
  }
  return withComma(ctx, marked(ctx, leafClass(value), leafText(value)), comma);
}

function leafText(value) {
  if (typeof value === "string") return JSON.stringify(value);
  if (typeof value === "number" || typeof value === "boolean") return String(value);
  return "null";
}

function leafClass(value) {
  if (typeof value === "string") return "json-string";
  if (typeof value === "number") return "json-number";
  if (typeof value === "boolean") return "json-boolean";
  return "json-null";
}

function withComma(ctx, el, comma) {
  if (!comma) return el;
  const frag = ctx.doc.createDocumentFragment();
  frag.append(el, span(ctx, "json-punct", ","));
  return frag;
}

// The comma after a folded value goes inside it, after its closing bracket,
// so it stays on that bracket's line.
function fold(ctx, open, close, members, size, comma, isOpen) {
  const doc = ctx.doc;
  if (members.length === 0) return withComma(ctx, span(ctx, "json-punct", open + close), comma);
  const details = doc.createElement("details");
  details.className = "json-fold";
  details.open = isOpen;
  const summary = doc.createElement("summary");
  summary.append(span(ctx, "json-punct", open), span(ctx, "json-size", size));
  const body = doc.createElement("div");
  body.className = "json-members";
  members.forEach(([key, v], i) => {
    const line = doc.createElement("div");
    line.className = "json-member";
    if (key !== null) line.append(marked(ctx, "json-key", JSON.stringify(key)), span(ctx, "json-punct", ": "));
    line.append(node(ctx, v, i < members.length - 1));
    body.append(line);
  });
  details.append(summary, body, span(ctx, "json-punct json-close", close + (comma ? "," : "")));
  return details;
}

/** Opens or closes every fold under root: expand all and collapse all. */
export function setFolds(root, open) {
  for (const d of root.querySelectorAll("details")) d.open = open;
}

/** What copy puts on the clipboard: the value as indented JSON. */
export const jsonText = (value) => JSON.stringify(value ?? null, null, 2);
