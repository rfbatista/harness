// The Tokens tab's model: the record's tokens as editable rows, what a row
// looks like (its sample), and the table's filter. A row holds its value as
// the text the person types; a composite value (typography, shadow) is typed
// as `key: value; key: value`. Pure.

/**
 * @typedef {{ id: string, origin: number | null, group: string, name: string, type: string, text: string, note: string }} Row
 * `origin` is the token's position in the saved record, null for a row the person added.
 */

/** The token types, in the order the type select lists them. */
export const TOKEN_TYPES = Object.freeze(["color", "dimension", "font", "number", "duration", "easing", "shadow", "typography"]);

const COMPOSITE = new Set(["shadow", "typography"]);
const isComposite = (type) => COMPOSITE.has(type);

/** The groups offered when naming a token's group, besides the ones in use. */
export const STANDARD_GROUPS = Object.freeze(["colors", "typography", "spacing", "radius", "shadows", "motion"]);

const KEY = /^[A-Za-z][A-Za-z0-9-]*$/;

/** The text a value is typed as. */
export function formatValue(type, value) {
  if (value !== null && typeof value === "object") {
    return Object.entries(value)
      .map(([k, v]) => `${k}: ${v}`)
      .join("; ");
  }
  return value == null ? "" : String(value);
}

/**
 * The value a row's text stands for. A composite part with no colon is kept
 * under an empty key, which the server refuses and names.
 */
export function parseValue(type, text) {
  if (!isComposite(type)) return text.trim();
  const out = {};
  for (const part of text.split(";").map((p) => p.trim()).filter((p) => p !== "")) {
    const at = part.indexOf(":");
    if (at < 0) out[""] = part;
    else out[part.slice(0, at).trim()] = part.slice(at + 1).trim();
  }
  return out;
}

/** @returns {Row} */
export function rowFromToken(token, id, origin) {
  return { id, origin, group: token.group, name: token.name, type: token.type, text: formatValue(token.type, token.value), note: token.note ?? "" };
}

/** @returns {Row[]} */
export function rowsFromRecord(record) {
  return (record?.tokens ?? []).map((t, i) => rowFromToken(t, `t${i}`, i));
}

/** @returns {Row} */
export function newRow(id, group, type) {
  return { id, origin: null, group, name: "", type, text: "", note: "" };
}

/** The token a row stands for, as the record holds it. */
export function tokenOf(row) {
  return { group: row.group.trim(), name: row.name.trim(), type: row.type, value: parseValue(row.type, row.text), note: row.note };
}

/** The record with the rows as its tokens; principles and components pass through. */
export function recordOf(rows, principles, components) {
  return { tokens: rows.map(tokenOf), principles, components };
}

const stable = (v) => JSON.stringify(v, (_, x) => (x && typeof x === "object" && !Array.isArray(x) ? Object.fromEntries(Object.entries(x).sort(([a], [b]) => (a < b ? -1 : 1))) : x));

/** "added" for a row the person added, "edited" for one that differs from its saved token, "" otherwise. */
export function rowStatus(row, saved) {
  if (row.origin === null) return "added";
  const was = saved[row.origin];
  if (!was) return "added";
  const now = tokenOf(row);
  const same = now.group === was.group && now.name === was.name && now.type === was.type && now.note === (was.note ?? "") && stable(now.value) === stable(was.value);
  return same ? "" : "edited";
}

/** How many rows were edited, added and removed against the saved tokens. */
export function changeCounts(rows, saved) {
  let edited = 0;
  let added = 0;
  let kept = 0;
  for (const row of rows) {
    const status = rowStatus(row, saved);
    if (status === "added") added++;
    else kept++;
    if (status === "edited") edited++;
  }
  return { edited, added, removed: Math.max(0, saved.length - kept) };
}

/** The rows matching the query (group, name, type, value or note) and, when set, the one group. */
export function filterRows(rows, query, group) {
  const q = query.trim().toLowerCase();
  return rows.filter((r) => {
    if (group !== "" && r.group.trim() !== group) return false;
    return q === "" || [r.group, r.name, r.type, r.text, r.note].some((s) => s.toLowerCase().includes(q));
  });
}

/** The groups in use with their row counts, in the order they first appear. */
export function groupCounts(rows) {
  const counts = new Map();
  for (const r of rows) {
    const g = r.group.trim();
    if (g !== "") counts.set(g, (counts.get(g) ?? 0) + 1);
  }
  return [...counts].map(([group, count]) => ({ group, count }));
}

const BOX = "display:inline-block;inline-size:1.25rem;block-size:1.25rem;vertical-align:middle;";
const HATCH = "background:repeating-linear-gradient(45deg,#fff,#fff 3px,#fcc 3px,#fcc 6px);";
const kebab = (key) => key.replace(/[A-Z]/g, (c) => `-${c.toLowerCase()}`);
// A value that could end the declaration it is written into is not drawn.
const drawable = (v) => v.trim() !== "" && !/[{};<>]/.test(v);

/**
 * What a row's sample looks like, drawn from the text as typed: its kind, an
 * inline style, a glyph, and whether the value could not be drawn.
 * `supports(property, value)` says whether the browser reads a colour.
 * @param {Row} row
 * @param {(property: string, value: string) => boolean} [supports]
 * @returns {{ kind: string, style: string, glyph: string, invalid: boolean }}
 */
export function sampleOf(row, supports = () => true) {
  const text = row.text.trim();
  const bad = (kind, style = BOX, glyph = "") => ({ kind, style, glyph, invalid: true });
  switch (row.type) {
    case "color":
      if (!drawable(text) || !supports("color", text)) return bad("swatch", `${BOX}border:1px solid var(--color-line-strong);border-radius:3px;${HATCH}`);
      return { kind: "swatch", style: `${BOX}border:1px solid var(--color-line-strong);border-radius:3px;background:${text}`, glyph: "", invalid: false };
    case "font":
      return drawable(text) ? { kind: "type", style: `font-family:${text};font-size:1.125rem`, glyph: "Aa", invalid: false } : bad("type", "", "Aa");
    case "typography": {
      const parts = Object.entries(parseValue("typography", text)).filter(([k, v]) => KEY.test(k) && drawable(v));
      return parts.length > 0
        ? { kind: "type", style: parts.map(([k, v]) => `${kebab(k)}:${v}`).join(";"), glyph: "Aa", invalid: false }
        : bad("type", "", "Aa");
    }
    case "dimension": {
      const group = row.group.trim();
      if (!drawable(text)) return bad("space");
      if (/radi|round/i.test(group)) {
        return { kind: "radius", style: `${BOX}border:2px solid var(--color-ink);border-radius:${text}`, glyph: "", invalid: false };
      }
      if (/^(type|font|text)/i.test(group)) return { kind: "type", style: `font-size:${text}`, glyph: "Aa", invalid: false };
      return { kind: "space", style: `display:inline-block;vertical-align:middle;inline-size:min(${text},2.5rem);block-size:min(${text},2.5rem);background:var(--color-signal);opacity:0.6`, glyph: "", invalid: false };
    }
    case "shadow": {
      const p = parseValue("shadow", text);
      if (Object.keys(p).length === 0 || Object.values(p).some((v) => !drawable(v))) return bad("shadow");
      const shadow = [p.offsetX || "0", p.offsetY || "0", p.blur || "0", p.spread || "0", p.color || "currentColor"].join(" ");
      return { kind: "shadow", style: `${BOX}border-radius:3px;background:var(--color-raised);box-shadow:${shadow}`, glyph: "", invalid: false };
    }
    case "duration": {
      const m = /^(-?[0-9]*\.?[0-9]+)(ms|s)$/.exec(text);
      if (!m) return bad("motion", "display:inline-block;inline-size:3rem;block-size:0.5rem;background:var(--color-sunken)");
      const ms = Number(m[1]) * (m[2] === "s" ? 1000 : 1);
      const pct = Math.max(0, Math.min(100, Math.round(ms / 10)));
      return { kind: "motion", style: `display:inline-block;vertical-align:middle;inline-size:3rem;block-size:0.5rem;border-radius:3px;background:linear-gradient(var(--color-signal),var(--color-signal)) 0 0 / ${pct}% 100% no-repeat,var(--color-sunken)`, glyph: "", invalid: false };
    }
    case "easing":
      return drawable(text)
        ? { kind: "motion", style: "display:inline-block;vertical-align:middle;inline-size:3rem;block-size:0.5rem;border-radius:3px;background:linear-gradient(var(--color-signal),var(--color-signal)) 0 0 / 60% 100% no-repeat,var(--color-sunken)", glyph: "", invalid: false }
        : bad("motion", "display:inline-block;inline-size:3rem;block-size:0.5rem;background:var(--color-sunken)");
    case "number":
      return { kind: "number", style: "font-family:var(--font-mono);color:var(--color-ink-muted)", glyph: "#", invalid: false };
    default:
      return { kind: "none", style: "", glyph: "", invalid: false };
  }
}

/** A placeholder that shows how a type's value is written. */
export function placeholderFor(type) {
  switch (type) {
    case "color":
      return "oklch(0.5 0.095 182)";
    case "dimension":
      return "0.75rem";
    case "font":
      return "Inter, sans-serif";
    case "number":
      return "1.25";
    case "duration":
      return "150ms";
    case "easing":
      return "cubic-bezier(0.2, 0, 0, 1)";
    case "shadow":
      return "offsetY: 2px; blur: 8px; color: #0003";
    case "typography":
      return "fontFamily: Inter, sans-serif; fontSize: 1.5rem";
    default:
      return "";
  }
}
