// The Components tab's model: the list of components, the one open, and a card
// for each of its previews. Pure.

import { missingTokens } from "./preview.js";

/**
 * @typedef {{ key: string, label: string, variant: string, state: string, preview: import("./preview.js").Preview, missing: string[], flagged: boolean }} Card
 * @typedef {{ slug: string, name: string, countText: string, missing: string[], flagged: boolean }} Item
 */

const plural = (n, word) => `${n} ${word}${n === 1 ? "" : "s"}`;

/**
 * The cards of one component: a preview each, with the tokens it names that
 * `tokens` lack.
 * @returns {Card[]}
 */
export function cardsOf(component, tokens) {
  return component.previews.map((preview, i) => {
    const missing = missingTokens(tokens, preview);
    return { key: `${component.slug}#${i}`, label: preview.label, variant: preview.variant, state: preview.state, preview, missing, flagged: missing.length > 0 };
  });
}

/**
 * The list rows, in record order.
 * @returns {Item[]}
 */
export function itemsOf(components, tokens) {
  return components.map((c) => {
    const missing = [...new Set(cardsOf(c, tokens).flatMap((card) => card.missing))];
    return { slug: c.slug, name: c.name, countText: plural(c.previews.length, "preview"), missing, flagged: missing.length > 0 };
  });
}

/** The component open: the one `slug` names, else the first; undefined when there are none. */
export function currentOf(components, slug) {
  return components.find((c) => c.slug === slug) ?? components[0];
}

/** The notes of a component as list rows. */
export const notesOf = (notes) => notes.map((text, i) => ({ key: String(i), text }));
