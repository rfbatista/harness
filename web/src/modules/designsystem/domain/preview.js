// The browser's copy of what the server does with component previews: the CSS
// custom property of a token, the `:root` rule, the preview document and the
// validation that guards a save. It is the one place the rule lives in JS;
// design-system/preview-parity.test.mjs holds it equal to the Go rule
// (internal/domain/designpreview.go) on fixed cases. Pure.

/**
 * @typedef {{ group: string, name: string, type: string, value: unknown, note?: string }} Token
 * @typedef {{ label: string, variant: string, state: string, html: string, css: string }} Preview
 * @typedef {{ slug: string, previews: Preview[] }} Component
 * @typedef {{ path: string, code: string, message: string }} FieldError
 */

/** The Content-Security-Policy of a preview document: no script, no network. */
export const PREVIEW_CSP = "default-src 'none'; style-src 'unsafe-inline'; img-src data:; font-src data:";

const NAME = /^[a-z][a-z0-9-]*$/;
const VAR_REF = /var\(\s*(--[A-Za-z0-9_-]+)/g;
const SCRIPT_TAG = /<\s*script/i;
const URL_CALL = /url\(\s*['"]?\s*(data:)?/gi;
const IMPORT_RULE = /@import/i;

/** `--<group>-<name>`, or `--<group>-<name>-<key>` (key in kebab case) for a member of a composite value. */
export function tokenProperty(group, name, part) {
  const base = `--${group}-${name}`;
  return part === "" ? base : `${base}-${part.replace(/[A-Z]/g, (c, at) => (at > 0 ? "-" : "") + c.toLowerCase())}`;
}

const isComposite = (value) => value !== null && typeof value === "object";

/** The custom properties and values a token defines, a composite's in property-name order. */
function declarations(token) {
  if (!isComposite(token.value)) return [[tokenProperty(token.group, token.name, ""), String(token.value)]];
  return Object.entries(token.value)
    .map(([k, v]) => [tokenProperty(token.group, token.name, k), String(v)])
    .sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0));
}

/** The `:root` rule defining every token, in record order. */
export function rootRule(tokens) {
  return `:root{${tokens.flatMap(declarations).map(([p, v]) => `${p}:${v}`).join(";")}}`;
}

/** The page a preview renders as: the server builds the same bytes. */
export function previewDocument(tokens, preview) {
  return (
    `<!doctype html><html><head><meta charset="utf-8">\n` +
    `<meta http-equiv="Content-Security-Policy" content="${PREVIEW_CSP}">\n` +
    `<style>${rootRule(tokens)}</style>\n` +
    `<style>${preview.css}</style></head><body>${preview.html}</body></html>`
  );
}

// hasEventAttribute reads start tags as a browser does: quotes hide a ">", and
// "/" and whitespace both end a name.
function hasEventAttribute(s) {
  const n = s.length;
  const blank = (ch) => /[\s/]/.test(ch);
  for (let i = 0; i < n; i++) {
    if (s[i] !== "<" || !/[A-Za-z]/.test(s[i + 1] ?? "")) continue;
    i++;
    while (i < n && !blank(s[i]) && s[i] !== ">") i++;
    for (;;) {
      while (i < n && blank(s[i])) i++;
      if (i >= n || s[i] === ">") break;
      let start = i;
      while (i < n && !blank(s[i]) && s[i] !== "=" && s[i] !== ">") i++;
      if (s.slice(start, i).toLowerCase().startsWith("on")) return true;
      while (i < n && /\s/.test(s[i])) i++;
      if (s[i] !== "=") continue;
      i++;
      while (i < n && /\s/.test(s[i])) i++;
      if (s[i] === '"' || s[i] === "'") {
        const close = s.indexOf(s[i], i + 1);
        i = close < 0 ? n : close + 1;
      } else {
        while (i < n && !/\s/.test(s[i]) && s[i] !== ">") i++;
      }
    }
  }
  return false;
}

/** Why a preview's html or css is refused; "" when it is fine. */
export function unsafePreviewProblem(s) {
  if (SCRIPT_TAG.test(s)) return "holds a <script>";
  if (hasEventAttribute(s)) return "holds an on* attribute";
  if (IMPORT_RULE.test(s)) return "holds an @import";
  for (const m of s.matchAll(URL_CALL)) if (!m[1]) return "holds a url( that is not a data: url";
  return "";
}

/**
 * What is wrong with the components of the record, as the server reports it:
 * the same codes and paths. `previous` is the record the save replaces: a
 * preview that uses a token it had and the record lacks is TOKEN_IN_USE.
 * @param {{ tokens: Token[], components: Component[] }} record
 * @param {{ tokens: Token[] }} [previous]
 * @returns {FieldError[]}
 */
export function validateComponents(record, previous = { tokens: [] }) {
  /** @type {FieldError[]} */
  const errs = [];
  const add = (path, code, message) => errs.push({ path, code, message });
  const known = new Set(record.tokens.flatMap((t) => declarations(t).map(([p]) => p)));
  const had = new Map(previous.tokens.flatMap((t) => declarations(t).map(([p]) => [p, `${t.group}.${t.name}`])));
  const slugs = new Set();
  /** @type {Map<string, string[]>} */
  const removed = new Map();
  record.components.forEach((c, i) => {
    const path = `/components/${i}`;
    if (!NAME.test(c.slug)) add(`${path}/slug`, "BAD_NAME", `slug ${JSON.stringify(c.slug)} must match ^[a-z][a-z0-9-]*$`);
    if (slugs.has(c.slug)) add(`${path}/slug`, "DUPLICATE_COMPONENT", `component ${JSON.stringify(c.slug)} is defined more than once`);
    slugs.add(c.slug);
    c.previews.forEach((p, j) => {
      const pp = `${path}/previews/${j}`;
      for (const [field, text] of [["html", p.html], ["css", p.css]]) {
        const msg = unsafePreviewProblem(text);
        if (msg) add(`${pp}/${field}`, "UNSAFE_PREVIEW", `${field} ${msg}`);
      }
      for (const m of p.css.matchAll(VAR_REF)) {
        if (known.has(m[1])) continue;
        const owner = had.get(m[1]);
        if (owner === undefined) {
          add(`${pp}/css`, "UNKNOWN_TOKEN", `${m[1]} is not a token`);
        } else {
          const users = removed.get(owner) ?? [];
          if (!users.includes(c.slug)) removed.set(owner, [...users, c.slug]);
        }
      }
    });
  });
  for (const owner of [...removed.keys()].sort()) add("/tokens", "TOKEN_IN_USE", `token ${owner} is used by ${removed.get(owner).join(", ")}`);
  return errs;
}
