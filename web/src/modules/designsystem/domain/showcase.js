// The Components tab's model: the list of components, the one open, a card
// for each of its previews, and the draft's edits. Components are addressed by
// their position, since the slug a person is typing is not yet an identity. Pure.

import { missingTokens } from "./preview.js";

/**
 * @typedef {import("./preview.js").Preview} Preview
 * @typedef {{ slug: string, name: string, purpose: string, do: string[], dont: string[], previews: Preview[] }} Component
 * @typedef {{ key: string, index: number, label: string, variant: string, state: string, preview: Preview, missing: string[], flagged: boolean }} Card
 * @typedef {{ key: string, index: number, slug: string, name: string, countText: string, missing: string[], flagged: boolean, problems: number }} Item
 */

const plural = (n, word) => `${n} ${word}${n === 1 ? "" : "s"}`;

/**
 * The cards of one component: a preview each, with the tokens it names that
 * `tokens` lack.
 * @param {Component} component
 * @param {number} at the component's position
 * @returns {Card[]}
 */
export function cardsOf(component, at, tokens) {
  return component.previews.map((preview, i) => {
    const missing = missingTokens(tokens, preview);
    return { key: `${at}#${i}`, index: i, label: preview.label, variant: preview.variant, state: preview.state, preview, missing, flagged: missing.length > 0 };
  });
}

/**
 * The list rows, in record order. `errors` are the draft's problems, by path.
 * @param {Component[]} components
 * @param {{ path: string }[]} [errors]
 * @returns {Item[]}
 */
export function itemsOf(components, tokens, errors = []) {
  return components.map((c, at) => {
    const missing = [...new Set(cardsOf(c, at, tokens).flatMap((card) => card.missing))];
    const problems = errors.filter((e) => e.path === `/components/${at}` || e.path.startsWith(`/components/${at}/`)).length;
    return { key: String(at), index: at, slug: c.slug, name: c.name, countText: plural(c.previews.length, "preview"), missing, flagged: missing.length > 0 || problems > 0, problems };
  });
}

/** The position of the component open: `at` when it is one, else the first; -1 when there are none. */
export function currentIndex(components, at) {
  if (components.length === 0) return -1;
  return at >= 0 && at < components.length ? at : 0;
}

/** The notes of a component as list rows. */
export const notesOf = (notes) => notes.map((text, i) => ({ key: String(i), text }));

const slugOf = (name) => name.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-+|-+$/g, "");

/** A component to start from: a valid slug no other uses, and one preview. */
export function newComponent(components) {
  const taken = new Set(components.map((c) => c.slug));
  let slug = "new-component";
  for (let n = 2; taken.has(slug); n++) slug = `new-component-${n}`;
  return { slug, name: "New component", purpose: "", do: [], dont: [], previews: [newPreview(0)] };
}

/** A preview to start from; `n` is how many the component has. */
export function newPreview(n) {
  return { label: n === 0 ? "Default" : `Variant ${n + 1}`, variant: "default", state: "default", html: "<p>Sample</p>", css: "" };
}

const lines = (notes) => notes.map((n) => n.trim()).filter((n) => n !== "");

/** A component with every field present, whatever the record held. */
const whole = (c) => ({ slug: c.slug ?? "", name: c.name ?? "", purpose: c.purpose ?? "", do: c.do ?? [], dont: c.dont ?? [], previews: c.previews ?? [] });

/** The components as the record holds them: names and notes trimmed, no blank notes. */
export function cleanComponents(components) {
  return components.map(whole).map((c) => ({
    slug: c.slug.trim(),
    name: c.name.trim(),
    purpose: c.purpose.trim(),
    do: lines(c.do),
    dont: lines(c.dont),
    previews: c.previews.map((p) => ({ label: p.label, variant: p.variant, state: p.state, html: p.html, css: p.css })),
  }));
}

/** Whether the draft's components differ from the saved ones. */
export const componentsChanged = (draft, saved) => JSON.stringify(cleanComponents(draft)) !== JSON.stringify(cleanComponents(saved));

/** A deep copy with every field present, so editing the draft never touches the saved record. */
export const copyComponents = (components) => JSON.parse(JSON.stringify(components)).map(whole);

const PATH = /^\/components\/(\d+)(?:\/previews\/(\d+))?(?:\/([a-z]+))?$/;

/** Where a problem's path is, in words: "button › Primary / default", "Tokens" or the fallback. */
export function problemLabel(path, components, fallback = "Design system") {
  if (path === "/tokens") return "Tokens";
  const m = PATH.exec(path);
  const c = m ? components[Number(m[1])] : undefined;
  if (!c) return fallback;
  const name = c.slug.trim() || "…";
  const p = m[2] === undefined ? undefined : c.previews[Number(m[2])];
  return `Components › ${p ? `${name} › ${p.label}` : name}`;
}
